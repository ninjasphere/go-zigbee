package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"

	"code.google.com/p/gogoprotobuf/proto"

	"github.com/ninjasphere/go-zigbee/nwkmgr"

	"github.com/ninjasphere/go-zigbee/gateway"
	"github.com/ninjasphere/go-zigbee/otasrvr"
)

const (
	hostname    = "beaglebone.local"
	otasrvrPort = 2525
	gatewayPort = 2541
	nwkmgrPort  = 2540
)

// ZStackServer holds the connection to one of the Z-Stack servers (nwkmgr, gateway and otasrvr)
type ZStackServer struct {
	Incoming chan *[]byte // Incoming raw protobuf packets

	name      string
	subsystem int8
	conn      net.Conn
	outgoing  chan *ZStackCommand // Outgoing protobuf messages
	pending   *ZStackPendingCommand
}

func (s *ZStackServer) sendCommand(request *ZStackCommand, response *ZStackCommand) error {

	s.outgoing <- request

	s.pending = &ZStackPendingCommand{
		response: response,
		complete: make(chan error),
	}

	error := <-s.pending.complete
	s.pending = nil
	return error
}

// ZStackOutgoingCommand is a thing
type ZStackPendingCommand struct {
	response *ZStackCommand
	complete chan error
}

// ZStackCommand contains a protobuf message and a command id
type ZStackCommand struct {
	message   proto.Message
	commandID int8
}

type ZStackNwkMgrServer struct {
	*ZStackServer
}

// SendRequest sends a protobuf Message to the Z-Stack server, and waits for the response
func (s *ZStackNwkMgrServer) SendCommand(request zStackNwkCommand, response zStackNwkCommand) error {

	return s.sendCommand(&ZStackCommand{
		message:   request,
		commandID: int8(request.GetCmdId()),
	}, &ZStackCommand{
		message:   response,
		commandID: int8(response.GetCmdId()),
	})

}

type pendingGatewayResponse struct {
	response zStackGatewayCommand
	finished chan error
}

type ZStackGatewayServer struct {
	*ZStackServer
	pendingResponses map[uint8]*pendingGatewayResponse
}

func (s *ZStackGatewayServer) WaitForSequenceResponse(sequenceNumber *uint32, response zStackGatewayCommand) error {
	number := uint8(*sequenceNumber) // We accept uint32 as thats what comes back from protobuf
	_, exists := s.pendingResponses[number]
	if exists {
		s.pendingResponses[number].finished <- fmt.Errorf("Another command with the same sequence id (%d) has been sent.", number)
	}

	pending := &pendingGatewayResponse{
		response: response,
		finished: make(chan error),
	}
	s.pendingResponses[number] = pending

	return <-pending.finished
}

// SendCommand sends a protobuf Message to the Z-Stack server, and waits for the response
func (s *ZStackGatewayServer) SendCommand(request zStackGatewayCommand, response zStackGatewayCommand) error {

	return s.sendCommand(&ZStackCommand{
		message:   request,
		commandID: int8(request.GetCmdId()),
	}, &ZStackCommand{
		message:   response,
		commandID: int8(response.GetCmdId()),
	})

}

func (s *ZStackGatewayServer) incomingZCLLoop() {

	for {
		bytes := <-s.Incoming

		log.Printf("gateway: Got gateway message % X", bytes)

		commandID := uint8((*bytes)[1])

		var message = &gateway.GwZigbeeGenericRspInd{} // Not always this, but it will always give us the sequence number?
		err := proto.Unmarshal(*bytes, message)
		if err != nil {
			log.Printf("gateway: Could not get sequence number from incoming gateway message : %s", err)
			continue
		}

		sequenceNumber := uint8(*message.SequenceNumber)

		log.Printf("gateway: Got an incoming gateway message, sequence:%d", sequenceNumber)

		if sequenceNumber == 0 {
			log.Printf("gateway: Failed to get a sequence number from an incoming gateway message. ????")
		}
		outJSON(message)

		pending := s.pendingResponses[sequenceNumber]

		if pending == nil {
			log.Printf("gateway: Received response to sequence number %d but we aren't listening for it", sequenceNumber)
		} else {

			if uint8(pending.response.GetCmdId()) != commandID {
				pending.finished <- fmt.Errorf("Wrong ZCL response type. Wanted: 0x%X Received: 0x%X", uint8(pending.response.GetCmdId()), commandID)
			}
			pending.finished <- proto.Unmarshal(*bytes, pending.response)
		}

	}

}

type zStackNwkCommand interface {
	proto.Message
	GetCmdId() nwkmgr.NwkMgrCmdIdT
}

type zStackGatewayCommand interface {
	proto.Message
	GetCmdId() gateway.GwCmdIdT
}

type zStackOtaMgrCommand interface {
	proto.Message
	GetCmdId() otasrvr.OtaMgrCmdIdT
}

func (s *ZStackServer) outgoingLoop() {
	for {
		command := <-s.outgoing

		proto.SetDefaults(command.message)

		packet, err := proto.Marshal(command.message)
		if err != nil {
			log.Fatal("marshaling error: ", err)
		}

		log.Printf("Protobuf packet %x", packet)

		buffer := new(bytes.Buffer)

		if err != nil {
			// handle error
			log.Printf("Error connecting %s", err)
		}

		// Add the Z-Stack 4-byte header
		err = binary.Write(buffer, binary.LittleEndian, uint16(len(packet))) // Packet length
		err = binary.Write(buffer, binary.LittleEndian, s.subsystem)         // Subsystem
		err = binary.Write(buffer, binary.LittleEndian, command.commandID)   // Command Id

		_, err = buffer.Write(packet)

		log.Printf("%s: Sending packet: % X", s.name, buffer.Bytes())

		// Send it to the Z-Stack server
		_, err = s.conn.Write(buffer.Bytes())
	}
}

func (s *ZStackServer) incomingLoop() {
	for {
		buf := make([]byte, 1024)
		n, err := s.conn.Read(buf)
		//log.Printf("Read %d from %s", n, s.name)
		if err != nil {
			log.Fatalf("%s: Error reading socket %s", s.name, err)
		}
		pos := 0

		for {
			var length uint16
			var incomingSubsystem uint8
			reader := bytes.NewReader(buf[pos:])
			err := binary.Read(reader, binary.LittleEndian, &length)
			if err != nil {
				log.Fatalf("%s: Failed to read packet length %s", s.name, err)
			}

			err = binary.Read(reader, binary.LittleEndian, &incomingSubsystem)
			if err != nil {
				log.Fatalf("%s: Failed to read packet subsystem %s", s.name, err)
			}

			log.Printf("%s: Incoming subsystem %d (wanted: %d)", s.name, incomingSubsystem, s.subsystem)

			log.Printf("%s: Found packet of size : %d", s.name, length)

			packet := buf[pos+4 : pos+4+int(length)]

			commandID := int8(packet[1])

			log.Printf("%s: Command ID:0x%X Packet: % X", s.name, commandID, packet)

			// Check if this packet has a ZCL request id... TODO

			if s.pending != nil {
				s.pending.complete <- proto.Unmarshal(packet, s.pending.response.message)
				s.pending = nil

			} else { // Or just send it out to be handled elsewhere
				s.Incoming <- &packet
			}

			pos += int(length) + 4

			if pos >= n {
				break
			}
		}
		//fmt.Printf("Received from %s (len:%d) : % X", s.name, n, buf[:n])
	}
}

func connectToServer(name string, subsystem int8, port int) (*ZStackServer, error) {

	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", hostname, port))

	if err != nil {
		return nil, err
	}

	server := &ZStackServer{
		name:      name,
		subsystem: subsystem,
		conn:      conn,
		outgoing:  make(chan *ZStackCommand),
		Incoming:  make(chan *[]byte),
	}

	go server.incomingLoop()
	go server.outgoingLoop()

	return server, nil
}

func main() {
	log.Println("Starting")

	_, err := connectToServer("otasrvr", int8(otasrvr.ZStackOTASysIDs_RPC_SYS_PB_OTA_MGR), otasrvrPort)
	if err != nil {
		// handle error
		log.Printf("Error connecting otasrvr %s", err)
	}

	nwkmgrTemp, err := connectToServer("nwkmgr", int8(nwkmgr.ZStackNwkMgrSysIdT_RPC_SYS_PB_NWK_MGR), nwkmgrPort)
	if err != nil {
		// handle error
		log.Printf("Error connecting nwkmgr %s", err)
	}
	nwkmgrConn := &ZStackNwkMgrServer{nwkmgrTemp}

	gatewayTemp, err := connectToServer("gateway", int8(gateway.ZStackGwSysIdT_RPC_SYS_PB_GW), gatewayPort)
	if err != nil {
		// handle error
		log.Printf("Error connecting gateway %s", err)
	}

	gatewayConn := &ZStackGatewayServer{
		ZStackServer:     gatewayTemp,
		pendingResponses: make(map[uint8]*pendingGatewayResponse),
	}
	go gatewayConn.incomingZCLLoop()

	//gatewayConn.pendingResponses = make(map[uint8]*pendingGatewayResponse)

	//command := &nwkmgr.NwkZigbeeNwkInfoReq{}
	response := &nwkmgr.NwkGetLocalDeviceInfoCnf{}
	err = nwkmgrConn.SendCommand(&nwkmgr.NwkGetLocalDeviceInfoReq{}, response)

	if err != nil {
		log.Fatalf("Failed to get local device info: %s", err)
	}
	log.Println("Local device info: ")
	outJSON(response)

	joinTime := uint32(30)
	permitJoinRequest := &nwkmgr.NwkSetPermitJoinReq{
		PermitJoinTime: &joinTime,
		PermitJoin:     nwkmgr.NwkPermitJoinTypeT_PERMIT_ALL.Enum(),
	}

	permitJoinResponse := &nwkmgr.NwkZigbeeGenericCnf{}

	err = nwkmgrConn.SendCommand(permitJoinRequest, permitJoinResponse)
	if err != nil {
		log.Fatalf("Failed to enable joining: %s", err)
	}
	if permitJoinResponse.Status.String() != "STATUS_SUCCESS" {
		log.Fatalf("Failed to enable joining: %s", permitJoinResponse.Status)
	}
	log.Println("Permit join response: ")
	outJSON(permitJoinResponse)

	deviceListResponse := &nwkmgr.NwkGetDeviceListCnf{}

	err = nwkmgrConn.SendCommand(&nwkmgr.NwkGetDeviceListReq{}, deviceListResponse)
	if err != nil {
		log.Fatalf("Failed to get device list: %s", err)
	}
	log.Printf("Found %d device(s): ", len(deviceListResponse.DeviceList))
	outJSON(deviceListResponse)

	for _, device := range deviceListResponse.DeviceList {
		log.Printf("Got device : %d", device.IeeeAddress)
		for _, endpoint := range device.SimpleDescList {
			log.Printf("Got endpoint : %d", endpoint.EndpointId)

			if containsUInt32(endpoint.InputClusters, 0x06) {
				log.Printf("This endpoint has on/off cluster")

				toggleRequest := &gateway.DevSetOnOffStateReq{
					DstAddress: &gateway.GwAddressStructT{
						AddressType: gateway.GwAddressTypeT_UNICAST.Enum(),
						IeeeAddr:    device.IeeeAddress,
					},
					State: gateway.GwOnOffStateT_TOGGLE_STATE.Enum(),
				}

				powerRequest := &gateway.DevGetPowerReq{
					DstAddress: &gateway.GwAddressStructT{
						AddressType: gateway.GwAddressTypeT_UNICAST.Enum(),
						IeeeAddr:    device.IeeeAddress,
					},
				}

				for i := 0; i < 3; i++ {
					log.Println("Toggling on/off device")

					confirmation := &gateway.GwZigbeeGenericCnf{}

					err = gatewayConn.SendCommand(toggleRequest, confirmation)
					if err != nil {
						log.Fatalf("Failed to toggle device: ", err)
					}
					log.Printf("Got on/off confirmation")
					if confirmation.Status.String() != "STATUS_SUCCESS" {
						log.Fatalf("Failed to request the device to toggle. Status:%s", confirmation.Status.String())
					}

					response := &gateway.GwZigbeeGenericRspInd{}
					err = gatewayConn.WaitForSequenceResponse(confirmation.SequenceNumber, response)
					if err != nil {
						log.Fatalf("Failed to get on/off response: ", err)
					}

					log.Printf("Got toggle response from device! Status: %s", response.Status.String())

					//time.Sleep(10000 * time.Millisecond)

					confirmation = &gateway.GwZigbeeGenericCnf{}

					err = gatewayConn.SendCommand(powerRequest, confirmation)
					if err != nil {
						log.Fatalf("Failed to request power: ", err)
					}
					log.Printf("Got power request confirmation")
					if confirmation.Status.String() != "STATUS_SUCCESS" {
						log.Fatalf("Failed to request the power. Status:%s", confirmation.Status.String())
					}

					powerResponse := &gateway.DevGetPowerRspInd{}
					err = gatewayConn.WaitForSequenceResponse(confirmation.SequenceNumber, powerResponse)
					if err != nil {
						log.Fatalf("Failed to get power response: ", err)
					}

					//	log.Printf("Got power level from device! Status: %s", powerResponse.Status.String())

					outJSON(powerResponse)

				}

			}

		}
		//GwOnOffStateT_TOGGLE_STATE
	}

	//time.Sleep(2000 * time.Millisecond)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, os.Kill)
	fmt.Println("Got signal:", <-c)

}

func containsUInt32(hackstack []uint32, needle uint32) bool {
	for _, cluster := range hackstack {
		if cluster == needle {
			return true
		}
	}
	return false
}

func outJSON(thing interface{}) {
	jsonOut, _ := json.Marshal(thing)

	log.Printf("%s", jsonOut)
}
