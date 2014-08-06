package zigbee

import (
	"code.google.com/p/gogoprotobuf/proto"
	"github.com/ninjasphere/go-zigbee/nwkmgr"
)

type ZStackNwkMgr struct {
	*ZStackServer
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

func ConnectToNwkMgrServer(hostname string, port int) (*ZStackNwkMgr, error) {
	server, err := connectToServer("NwkMgr", uint8(nwkmgr.ZStackNwkMgrSysIdT_RPC_SYS_PB_NWK_MGR), hostname, port)
	if err != nil {
		return nil, err
	}

	return &ZStackNwkMgr{server}, nil
}
