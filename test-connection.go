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

	name               string
	subsystem          int8
	conn               net.Conn
	outgoing           chan *ZStackCommand // Outgoing protobuf messages
	pendingByCommandID map[int8]*ZStackPendingResponse
}

func (s *ZStackServer) sendCommand(message *ZStackCommand) {
	s.outgoing <- message
}

func (s *ZStackServer) sendRequest(request *ZStackCommand, response *ZStackCommand) (chan error, error) {
	s.outgoing <- request

	pending := &ZStackPendingResponse{
		message:  response.message,
		complete: make(chan error),
	}

	if s.pendingByCommandID[response.commandID] != nil {
		return nil, fmt.Errorf("There is already a pending command waiting for the same response (Command ID: %X)", response.commandID)
	}

	s.pendingByCommandID[response.commandID] = pending

	return pending.complete, nil
}

// ZStackCommand contains a protobuf message and a command id
type ZStackCommand struct {
	message   proto.Message
	commandID int8
}

// ZStackPendingResponse contains the protobuf command to be filled with the response, and a 'complete' channel to indicate when done
type ZStackPendingResponse struct {
	message  proto.Message
	complete chan error
}

type ZStackNwkMgrServer struct {
	*ZStackServer
}

// SendCommand sends a protobuf Message to the Z-Stack server
func (s *ZStackNwkMgrServer) SendCommand(command zStackNwkCommand) {
	s.sendCommand(&ZStackCommand{
		message:   command,
		commandID: int8(command.GetCmdId()),
	})
}

// SendRequest sends a protobuf Message to the Z-Stack server, and waits for the response
func (s *ZStackNwkMgrServer) SendRequest(request zStackNwkCommand, response zStackNwkCommand) error {

	complete, err := s.sendRequest(&ZStackCommand{
		message:   request,
		commandID: int8(request.GetCmdId()),
	}, &ZStackCommand{
		message:   response,
		commandID: int8(response.GetCmdId()),
	})

	if err != nil {
		return err
	}

	return <-complete
}

type zStackNwkCommand interface {
	proto.Message
	GetCmdId() nwkmgr.NwkMgrCmdIdT
}

type zStackGatewayMessage interface {
	GetCmdId() gateway.GwCmdIdT
}

type zStackOtaMgrMessage interface {
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

			// Check if this packet has a ZCL request id... TODO

			// Check if we have any pending requests that want this command id...
			pending := s.pendingByCommandID[commandID]

			if pending != nil {
				pending.complete <- proto.Unmarshal(packet, pending.message)

			} else { // Or just send it out to be handled elsewhere
				s.Incoming <- &packet
			}

			log.Printf("%s: Command ID:0x%X Packet: % X", s.name, commandID, packet)
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
		name:               name,
		subsystem:          subsystem,
		conn:               conn,
		outgoing:           make(chan *ZStackCommand),
		Incoming:           make(chan *[]byte),
		pendingByCommandID: make(map[int8]*ZStackPendingResponse),
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

	gatewayConn, err := connectToServer("gateway", int8(gateway.ZStackGwSysIdT_RPC_SYS_PB_GW), gatewayPort)
	if err != nil {
		// handle error
		log.Printf("Error connecting gateway %s", err)
	}

	go func() {
		for {
			bytes := <-gatewayConn.Incoming

			log.Printf("Got gateway message % X", bytes)

			var message = &gateway.GwAttributeReportingInd{}
			err = proto.Unmarshal(*bytes, message)
			outJSON(message)

		}
	}()

	//command := &nwkmgr.NwkZigbeeNwkInfoReq{}
	response := &nwkmgr.NwkGetLocalDeviceInfoCnf{}
	err = nwkmgrConn.SendRequest(&nwkmgr.NwkGetLocalDeviceInfoReq{}, response)

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

	err = nwkmgrConn.SendRequest(permitJoinRequest, permitJoinResponse)
	if err != nil {
		log.Fatalf("Failed to enable joining: %s", err)
	}
	log.Println("Permit join response: ")
	outJSON(permitJoinResponse)

	deviceListResponse := &nwkmgr.NwkGetDeviceListCnf{}

	err = nwkmgrConn.SendRequest(&nwkmgr.NwkGetDeviceListReq{}, deviceListResponse)
	if err != nil {
		log.Fatalf("Failed to get device list: %s", err)
	}
	log.Println("Device list: ")
	outJSON(deviceListResponse)

	//time.Sleep(2000 * time.Millisecond)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, os.Kill)
	fmt.Println("Got signal:", <-c)

}

func outJSON(thing interface{}) {
	jsonOut, _ := json.Marshal(thing)

	log.Printf("%s", jsonOut)
}
