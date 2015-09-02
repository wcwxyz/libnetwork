package windows

import (
	"github.com/docker/libnetwork/datastore"
	"github.com/docker/libnetwork/driverapi"
)

const networkType = "windows"

// TODO Windows. This is a placeholder for now

type driver struct{}

// Init registers a new instance of null driver
func Init(dc driverapi.DriverCallback) error {
	c := driverapi.Capability{
		DataScope: datastore.LocalScope,
	}
	return dc.RegisterDriver(networkType, &driver{}, c)
}

func (d *driver) Config(option map[string]interface{}) error {
	return nil
}

func (d *driver) CreateNetwork(id string, option map[string]interface{}) error {
	return nil
}

func (d *driver) DeleteNetwork(nid string) error {
	return nil
}

func (d *driver) CreateEndpoint(nid, eid string, epInfo driverapi.EndpointInfo, epOptions map[string]interface{}) error {
	return nil
}

func (d *driver) DeleteEndpoint(nid, eid string) error {
	return nil
}

func (d *driver) EndpointOperInfo(nid, eid string) (map[string]interface{}, error) {
	return make(map[string]interface{}, 0), nil
}

// Join method is invoked when a Sandbox is attached to an endpoint.
func (d *driver) Join(nid, eid string, sboxKey string, jinfo driverapi.JoinInfo, options map[string]interface{}) error {
	return nil
}

// Leave method is invoked when a Sandbox detaches from an endpoint.
func (d *driver) Leave(nid, eid string) error {
	return nil
}

func (d *driver) Type() string {
	return networkType
}
