package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"time"

	"code.google.com/p/gogoprotobuf/proto"

	"github.com/ninjasphere/go-zigbee/nwkmgr"

	"github.com/ninjasphere/go-zigbee/gateway"
	"github.com/ninjasphere/go-zigbee/otasrvr"
)

const (
  hostname = "10.0.1.164"
  otasrvrPort  = 2525
  gatewayPort = 2541
  nwkmgrPort = 2540
)

// ZStackServer holds the connection to one of the Z-Stack servers (nwkmgr, gateway and otasrvr)
type ZStackServer struct {
	name      string
	subsystem int8
	conn      net.Conn
}

func connectToServer(name string, subsystem int8, port int) (*ZStackServer, error) {

	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", hostname, port))

	if err != nil {
		return nil, err
	}

	go func() {
		for {
			log.Printf("Reading from %s", name)
			buf := make([]byte, 1024)
			n, err := conn.Read(buf)
			if err != nil {
				log.Fatalf("whoops %s", err)
			}
			fmt.Printf("Received from %s (len:%d) : % X", name, n, buf[:n])
		}
	}()

	return &ZStackServer{
		name:      name,
		subsystem: subsystem,
		conn:      conn,
	}, nil
}

func main() {
	log.Println("Starting")

	_, err := connectToServer("otasrvr", int8(otasrvr.ZStackOTASysIDs_RPC_SYS_PB_OTA_MGR), otasrvrPort)
	if err != nil {
		// handle error
		log.Printf("Error connecting otasrvr %s", err)
	}

	nwkmgrConn, err := connectToServer("nwkmgr", int8(nwkmgr.ZStackNwkMgrSysIdT_RPC_SYS_PB_NWK_MGR), nwkmgrPort)
	if err != nil {
		// handle error
		log.Printf("Error connecting nwkmgr %s", err)
	}
	_, err = connectToServer("gateway", int8(gateway.ZStackGwSysIdT_RPC_SYS_PB_GW), gatewayPort)
	if err != nil {
		// handle error
		log.Printf("Error connecting gateway %s", err)
	}

	/*joinTime := uint32(30)
	command := &nwkmgr.NwkSetPermitJoinReq{
		PermitJoinTime: &joinTime,
		PermitJoin:     nwkmgr.NwkPermitJoinTypeT_PERMIT_ALL.Enum(),
	}*/

	//command := &nwkmgr.NwkZigbeeNwkInfoReq{}
	command := &nwkmgr.NwkGetLocalDeviceInfoReq{}

	time.Sleep(2000 * time.Millisecond)

	nwkmgrConn.sendCommand(command, int8(command.GetCmdId()))

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, os.Kill)
	fmt.Println("Got signal:", <-c)

}

func (s *ZStackServer) sendCommand(command proto.Message, commandID int8) {
	proto.SetDefaults(command)

	packet, err := proto.Marshal(command)
	if err != nil {
		log.Fatal("marshaling error: ", err)
	}

	log.Printf("Protobuf packet %x", packet)

	buffer := new(bytes.Buffer)

	if err != nil {
		// handle error
		log.Printf("Error connecting %s", err)
	}

	err = binary.Write(buffer, binary.LittleEndian, uint16(len(packet)))                                // Packet length
	err = binary.Write(buffer, binary.LittleEndian, int8(nwkmgr.ZStackNwkMgrSysIdT_RPC_SYS_PB_NWK_MGR)) // Subsystem
	err = binary.Write(buffer, binary.LittleEndian, int8(commandID))                                    // Command Id
	_, err = buffer.Write(packet)

	log.Printf("%s: Sending packet: % X", s.name, buffer.Bytes())
	//fmt.Fprintf(conn, "GET / HTTP/1.0\r\n\r\n")
	//status, err := bufio.NewReader(conn).ReadString('\n')

	_, err = s.conn.Write(buffer.Bytes())
}
