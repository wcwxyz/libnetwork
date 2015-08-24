package libnetwork

import (
	"os"
	"testing"

	"github.com/docker/libkv/store"
	"github.com/docker/libnetwork/config"
	"github.com/docker/libnetwork/datastore"
	"github.com/docker/libnetwork/options"
)

func TestBoltdbBackend(t *testing.T) {
	testLocalBackend(t, "", "", nil)
	os.Remove(defaultLocalStoreConfig.Client.Address)
	testLocalBackend(t, "boltdb", "/tmp/boltdb.db", &store.Config{Bucket: "testBackend"})
	os.Remove("/tmp/boltdb.db")
}

func testLocalBackend(t *testing.T, provider, url string, storeConfig *store.Config) {
	netOptions := []config.Option{}
	netOptions = append(netOptions, config.OptionLocalKVProvider(provider))
	netOptions = append(netOptions, config.OptionLocalKVProviderURL(url))
	netOptions = append(netOptions, config.OptionLocalKVProviderConfig(storeConfig))

	ctrl, err := New(netOptions...)
	if err != nil {
		t.Fatalf("Error new controller: %v", err)
	}
	if err := ctrl.ConfigureNetworkDriver("host", options.NewGeneric()); err != nil {
		t.Fatalf("Error initializing host driver: %v", err)
	}
	nw, err := ctrl.NewNetwork("host", "host")
	if err != nil {
		t.Fatalf("Error creating default \"host\" network: %v", err)
	}
	ep, err := nw.CreateEndpoint("newendpoint", []EndpointOption{}...)
	if err != nil {
		t.Fatalf("Error creating endpoint: %v", err)
	}
	store := ctrl.(*controller).localStore.KVStore()
	if exists, err := store.Exists(datastore.Key(datastore.NetworkKeyPrefix, string(nw.ID()))); !exists || err != nil {
		t.Fatalf("Network key should have been created.")
	}
	if exists, err := store.Exists(datastore.Key([]string{datastore.EndpointKeyPrefix, string(nw.ID()), string(ep.ID())}...)); !exists || err != nil {
		t.Fatalf("Endpoint key should have been created.")
	}
	store.Close()

	// test restore of local store
	ctrl, err = New(netOptions...)
	if nw, err = ctrl.NetworkByID(nw.ID()); err != nil {
		t.Fatalf("Error get network %v", err)
	}
	if ep, err = nw.EndpointByID(ep.ID()); err != nil {
		t.Fatalf("Error get endpoint %v", err)
	}
	if err := ep.Delete(); err != nil {
		t.Fatalf("Error delete endpoint %v", err)
	}
	store = ctrl.(*controller).localStore.KVStore()
	if exists, err := store.Exists(datastore.Key([]string{datastore.EndpointKeyPrefix, string(nw.ID()), string(ep.ID())}...)); exists || err != nil {
		t.Fatalf("Endpoint key should have been deleted. ")
	}
}
