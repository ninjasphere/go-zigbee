package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"os"

	"code.google.com/p/gogoprotobuf/proto"

	"github.com/ninjasphere/go-zigbee/nwkmgr"
)

const (
	hostname    = "beaglebone.local"
	nwkmgrPort  = 2536
	gatewayPort = 2541
	otaPort     = 2540
)

func main() {
	log.Println("Starting")

	/*joinTime := uint32(30)
	command := &nwkmgr.NwkSetPermitJoinReq{
		PermitJoinTime: &joinTime,
		PermitJoin:     nwkmgr.NwkPermitJoinTypeT_PERMIT_ALL.Enum(),
	}*/

	command := &nwkmgr.NwkZigbeeNwkInfoReq{}

	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", hostname, nwkmgrPort))

	sendNwkMgrCommand(command, command.GetCmdId(), conn)

	reply := make([]byte, 1)

	_, err = conn.Read(reply)
	if err != nil {
		println("Read from server failed:", err.Error())
		os.Exit(1)
	}

	println("reply from server=", string(reply))

}

func sendNwkMgrCommand(command proto.Message, commandID nwkmgr.NwkMgrCmdIdT, conn net.Conn) {
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

	log.Printf("Created packet %x", buffer.Bytes())
	//fmt.Fprintf(conn, "GET / HTTP/1.0\r\n\r\n")
	//status, err := bufio.NewReader(conn).ReadString('\n')

	_, err = conn.Write(buffer.Bytes())

}
