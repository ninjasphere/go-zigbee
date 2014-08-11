package zigbee

import (
	"fmt"
	"log"

	"code.google.com/p/gogoprotobuf/proto"
	"github.com/davecgh/go-spew/spew"
	"github.com/ninjasphere/go-zigbee/nwkmgr"
)

type ZStackNwkMgr struct {
	*ZStackServer
	OnDeviceFound func(deviceInfo *nwkmgr.NwkDeviceInfoT)
}

type zStackNwkMgrCommand interface {
	proto.Message
	GetCmdId() nwkmgr.NwkMgrCmdIdT
}

// SendCommand sends a protobuf Message to the Z-Stack server, and waits for the response
func (s *ZStackNwkMgr) SendCommand(request zStackNwkMgrCommand, response zStackNwkMgrCommand) error {

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

func (d *ZStackNwkMgr) FetchDeviceList() error {
	deviceListResponse := &nwkmgr.NwkGetDeviceListCnf{}

	err := d.SendCommand(&nwkmgr.NwkGetDeviceListReq{}, deviceListResponse)
	if err != nil {
		log.Fatalf("Failed to get device list: %s", err)
	}
	log.Printf("Found %d device(s): ", len(deviceListResponse.DeviceList))

	for _, deviceInfo := range deviceListResponse.DeviceList {
		d.OnDeviceFound(deviceInfo)
	}

	return nil
}

func (s *ZStackNwkMgr) Reset(hard bool) error {

	log.Printf("Resetting. Hard: %t", hard)

	mode := nwkmgr.NwkResetModeT_SOFT_RESET.Enum()
	if hard {
		mode = nwkmgr.NwkResetModeT_HARD_RESET.Enum()
	}

	response := nwkmgr.NwkZigbeeSystemResetCnf{}

	err := s.SendCommand(&nwkmgr.NwkZigbeeSystemResetReq{
		Mode: mode,
	}, &response)

	if err != nil {
		return err
	}

	if response.Status.String() != "STATUS_SUCCESS" {
		return fmt.Errorf("Invalid confirmation status: %s", response.Status.String())
	}

	return nil
}

func (s *ZStackNwkMgr) onIncoming(commandID uint8, bytes *[]byte) {

	//bytes := <-s.Incoming

	log.Printf("nwkmgr: Got nwkmgr message % X", bytes)

	switch commandID {
	case uint8(nwkmgr.NwkMgrCmdIdT_NWK_ZIGBEE_DEVICE_IND):
		device := &nwkmgr.NwkZigbeeDeviceInd{}

		err := proto.Unmarshal(*bytes, device)
		if err != nil {
			log.Printf("nwkmgr: Failed to read device announcement : %s", err)
			return
		}

		s.OnDeviceFound(device.DeviceInfo)
	default:
		log.Println("nwkmgr: Unknown incoming network manager message!")
	}

}

func ConnectToNwkMgrServer(hostname string, port int) (*ZStackNwkMgr, error) {
	server, err := connectToServer("NwkMgr", uint8(nwkmgr.ZStackNwkMgrSysIdT_RPC_SYS_PB_NWK_MGR), hostname, port)
	if err != nil {
		return nil, err
	}

	nwkmgr := &ZStackNwkMgr{
		ZStackServer: server,
		OnDeviceFound: func(deviceInfo *nwkmgr.NwkDeviceInfoT) {
			log.Println("nwkmgr: Warning: Device found. You must add an onDeviceFound handler!")
			spew.Dump(deviceInfo)
		},
	}

	server.onIncoming = func(commandID uint8, bytes *[]byte) {
		nwkmgr.onIncoming(commandID, bytes)
	}

	return nwkmgr, nil
}
