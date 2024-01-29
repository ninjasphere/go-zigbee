package zigbee

import (
	"github.com/nps5696/go-ninja/logger"
)

var (
	log = logger.GetLogger("go-openzwave") // usually replaced by driver
)

func SetLogger(logger *logger.Logger) {
	log = logger
}
