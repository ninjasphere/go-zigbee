package zigbee

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"code.google.com/p/gogoprotobuf/proto"
)

// ZStackServer holds the connection to one of the Z-Stack servers (nwkmgr, gateway and otasrvr)
type ZStackServer struct {
	name      string
	subsystem uint8
	conn      net.Conn
	pending   *zStackPendingCommand

	outgoingSync *sync.Mutex

	onIncoming func(uint8, *[]byte)
}

// ZStackPendingCommand is a thing
type zStackPendingCommand struct {
	request  *zStackCommand
	response *zStackCommand
	complete chan error
}

// ZStackCommand contains a protobuf message and a command id
type zStackCommand struct {
	message   proto.Message
	commandID uint8
}

func (s *ZStackServer) sendCommand(request *zStackCommand, response *zStackCommand) error {

	s.outgoingSync.Lock()

	s.pending = &zStackPendingCommand{
		request:  request,
		response: response,
		complete: make(chan error),
	}

	err := s.transmitCommand(request)

	if err == nil {
		// The command was sent sucessfully, so we wait for the response
		timeout := make(chan bool, 1)
		go func() {
			time.Sleep(1 * time.Second) // All commands should return immediately with at least a confirmation
			timeout <- true
		}()

		select {
		case error := <-s.pending.complete:
			err = error
		case <-timeout:
			err = fmt.Errorf("The request timed out")
		}
	}

	s.pending = nil
	s.outgoingSync.Unlock()

	return err
}

func (s *ZStackServer) transmitCommand(command *zStackCommand) error {

	proto.SetDefaults(command.message)

	packet, err := proto.Marshal(command.message)
	if err != nil {
		log.Fatalf("%s: Outgoing marshaling error: %s", s.name, err)
	}

	log.Printf("Protobuf packet %x", packet)

	buffer := new(bytes.Buffer)

	// Add the Z-Stack 4-byte header
	err = binary.Write(buffer, binary.LittleEndian, uint16(len(packet))) // Packet length
	err = binary.Write(buffer, binary.LittleEndian, s.subsystem)         // Subsystem
	err = binary.Write(buffer, binary.LittleEndian, command.commandID)   // Command Id

	_, err = buffer.Write(packet)

	log.Printf("%s: Sending packet: % X", s.name, buffer.Bytes())

	// Send it to the Z-Stack server
	_, err = s.conn.Write(buffer.Bytes())
	return err
}

func (s *ZStackServer) incomingLoop() {
	for {
		buf := make([]byte, 1024)
		n, err := s.conn.Read(buf)

		log.Printf("Read %d from %s", n, s.name)
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

			commandID := int8(buf[pos+3])

			packet := buf[pos+4 : pos+4+int(length)]

			log.Printf("%s: Command ID:0x%X Packet: % X", s.name, commandID, packet)

			if s.pending != nil {
				s.pending.complete <- proto.Unmarshal(packet, s.pending.response.message)
				s.pending = nil
			} else if s.onIncoming != nil { // Or just send it out to be handled elsewhere
				go s.onIncoming(uint8(commandID), &packet)
			} else {
				log.Printf("%s: ERR: Unhandled incoming packet", s.name)
			}

			pos += int(length) + 4

			if pos >= n {
				break
			}
		}
		//fmt.Printf("Received from %s (len:%d) : % X", s.name, n, buf[:n])
	}
}

func connectToServer(name string, subsystem uint8, hostname string, port int) (*ZStackServer, error) {

	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", hostname, port))

	if err != nil {
		return nil, err
	}

	server := &ZStackServer{
		name:         name,
		subsystem:    subsystem,
		conn:         conn,
		outgoingSync: &sync.Mutex{},
	}

	go server.incomingLoop()

	return server, nil
}
