package zigbee

import (
	"fmt"
	"log"

	"code.google.com/p/gogoprotobuf/proto"
	"github.com/ninjasphere/go-zigbee/gateway"
)

type ZStackGateway struct {
	*ZStackServer
	pendingResponses map[uint8]*pendingGatewayResponse
}

type zStackGatewayCommand interface {
	proto.Message
	GetCmdId() gateway.GwCmdIdT
}

type pendingGatewayResponse struct {
	response zStackGatewayCommand
	finished chan error
}

func (s *ZStackGateway) WaitForSequenceResponse(sequenceNumber *uint32, response zStackGatewayCommand) error {
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
func (s *ZStackGateway) SendCommand(request zStackGatewayCommand, response zStackGatewayCommand) error {

	return s.sendCommand(
		&zStackCommand{
			message:   request,
			commandID: uint8(request.GetCmdId()),
		},
		&zStackCommand{
			message:   response,
			commandID: uint8(response.GetCmdId()),
		},
	)

}

func (s *ZStackGateway) incomingZCLLoop() {

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

func ConnectToGatewayServer(hostname string, port int) (*ZStackGateway, error) {
	server, err := connectToServer("Gateway", uint8(gateway.ZStackGwSysIdT_RPC_SYS_PB_GW), hostname, port)
	if err != nil {
		return nil, err
	}

	gateway := &ZStackGateway{
		ZStackServer:     server,
		pendingResponses: make(map[uint8]*pendingGatewayResponse),
	}

	go gateway.incomingZCLLoop()

	return gateway, nil
}
