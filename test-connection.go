// +build ignore

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"

	zigbee "github.com/ninjasphere/go-zigbee"

	"github.com/ninjasphere/go-zigbee/nwkmgr"

	"github.com/ninjasphere/go-zigbee/gateway"
)

const (
	hostname    = "beaglebone.local"
	otasrvrPort = 2525
	gatewayPort = 2541
	nwkmgrPort  = 2540
)

func main() {
	log.Println("Starting")

	_, err := zigbee.ConnectToOtaServer(hostname, otasrvrPort)
	if err != nil {
		// handle error
		log.Printf("Error connecting otasrvr %s", err)
	}

	nwkmgrConn, err := zigbee.ConnectToNwkMgrServer(hostname, nwkmgrPort)
	if err != nil {
		// handle error
		log.Printf("Error connecting nwkmgr %s", err)
	}

	gatewayConn, err := zigbee.ConnectToGatewayServer(hostname, gatewayPort)
	if err != nil {
		// handle error
		log.Printf("Error connecting nwkmgr %s", err)
	}

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
