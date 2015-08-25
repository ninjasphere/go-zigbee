// +build ignore

package main

import (
	"encoding/json"
	"log"

	"github.com/gogo/protobuf/proto"

	"github.com/ninjasphere/go-zigbee/nwkmgr"
)

func main() {

	joinTime := uint32(30)
	permitRequest := &nwkmgr.NwkSetPermitJoinReq{
		PermitJoinTime: &joinTime,
		PermitJoin:     nwkmgr.NwkPermitJoinTypeT_PERMIT_ALL.Enum(),
	}

	proto.SetDefaults(permitRequest)

	data, err := proto.Marshal(permitRequest)
	if err != nil {
		log.Fatal("marshaling error: ", err)
	}

	log.Printf("marshalled permit join to protobuf: %x", data)

	jsonData, err := json.Marshal(permitRequest)
	if err != nil {
		log.Fatal("marshaling error: ", err)
	}

	log.Printf("marshalled permit join to json: %s", jsonData)

	newTest := new(nwkmgr.NwkSetPermitJoinReq)
	err = proto.Unmarshal(data, newTest)
	if err != nil {
		log.Fatal("unmarshaling error: ", err)
	}
	if permitRequest.GetPermitJoinTime() != newTest.GetPermitJoinTime() {
		log.Fatalf("data mismatch %d != %d", permitRequest.GetPermitJoinTime(), newTest.GetPermitJoinTime())
	}

}
