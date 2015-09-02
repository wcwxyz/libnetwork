package libnetwork

import (
	"encoding/json"
	"fmt"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/libkv/store"
	"github.com/docker/libnetwork/config"
	"github.com/docker/libnetwork/datastore"
)

var (
	defaultLocalStoreConfig = config.DatastoreCfg{
		Embedded: true,
		Client: config.DatastoreClientCfg{
			Provider: "boltdb",
			Address:  defaultPrefix + "/boltdb.db",
			Config: &store.Config{
				Bucket: "libnetwork",
			},
		},
	}
)

func (c *controller) validateGlobalStoreConfig() bool {
	return c.cfg != nil && c.cfg.GlobalStore.Client.Provider != "" && c.cfg.GlobalStore.Client.Address != ""
}

func (c *controller) initGlobalStore() error {
	c.Lock()
	cfg := c.cfg
	c.Unlock()
	if !c.validateGlobalStoreConfig() {
		return fmt.Errorf("globalstore initialization requires a valid configuration")
	}

	store, err := datastore.NewDataStore(&cfg.GlobalStore)
	if err != nil {
		return err
	}
	c.Lock()
	c.globalStore = store
	c.Unlock()

	nws, err := c.getNetworksFromGlobalStore()
	if err == nil {
		c.processNetworkUpdate(nws, nil)
	} else if err != datastore.ErrKeyNotFound {
		log.Warnf("failed to read networks from globalstore during init : %v", err)
	}
	return c.watchNetworks()
}

func (c *controller) initLocalStore() error {
	c.Lock()
	cfg := c.cfg
	c.Unlock()
	localStore, err := datastore.NewDataStore(c.getLocalStoreConfig(cfg))
	if err != nil {
		return err
	}
	c.Lock()
	c.localStore = localStore
	c.Unlock()

	nws, err := c.getNetworksFromLocalStore()
	if err == nil {
		c.processNetworkUpdate(nws, nil)
	} else if err != datastore.ErrKeyNotFound {
		log.Warnf("failed to read networks from localstore during init : %v", err)
	}
	eps, err := c.getEndpointsFromLocalStore()
	if err == nil {
		c.processEndpointsUpdate(eps, nil)
	} else if err != datastore.ErrKeyNotFound {
		log.Warnf("failed to read endpoints from localstore during init : %v", err)
	}
	return nil
}

func (c *controller) getNetworksFromGlobalStore() ([]*store.KVPair, error) {
	c.Lock()
	cs := c.globalStore
	c.Unlock()
	return cs.KVStore().List(datastore.Key(datastore.NetworkKeyPrefix))
}

func (c *controller) newNetworkFromStore(n *network) error {
	n.Lock()
	n.ctrlr = c
	n.endpoints = endpointTable{}
	n.Unlock()

	return c.addNetwork(n)
}

func (c *controller) updateNetworkToStore(n *network) error {
	cs := c.getDataStore(n.DataScope())
	if cs == nil {
		log.Debugf("datastore not initialized. Network %s is not added to the store", n.Name())
		return nil
	}

	return cs.PutObjectAtomic(n)
}

func (c *controller) deleteNetworkFromStore(n *network) error {
	cs := c.getDataStore(n.DataScope())
	if cs == nil {
		log.Debugf("datastore not initialized. Network %s is not deleted from datastore", n.Name())
		return nil
	}

	if err := cs.DeleteObjectAtomic(n); err != nil {
		return err
	}

	return nil
}

func (c *controller) newEndpointFromStore(key string, ep *endpoint) error {
	ep.Lock()
	n := ep.network
	id := ep.id
	ep.Unlock()

	_, err := n.EndpointByID(id)
	if err != nil {
		if _, ok := err.(ErrNoSuchEndpoint); ok {
			return n.addEndpoint(ep)
		}
	}
	return err
}

func (c *controller) updateEndpointToStore(ep *endpoint) error {
	ep.Lock()
	name := ep.name
	ep.Unlock()
	cs := c.getDataStore(ep.DataScope())
	if cs == nil {
		log.Debugf("datastore not initialized. endpoint %s is not added to the store", name)
		return nil
	}

	return cs.PutObjectAtomic(ep)
}

func (c *controller) deleteEndpointFromStore(ep *endpoint) error {
	cs := c.getDataStore(ep.DataScope())
	if cs == nil {
		log.Debugf("datastore not initialized. endpoint %s is not deleted from datastore", ep.Name())
		return nil
	}

	if err := cs.DeleteObjectAtomic(ep); err != nil {
		return err
	}

	return nil
}

func (c *controller) watchNetworks() error {
	if !c.validateGlobalStoreConfig() {
		return nil
	}

	c.Lock()
	cs := c.globalStore
	c.Unlock()

	nwPairs, err := cs.KVStore().WatchTree(datastore.Key(datastore.NetworkKeyPrefix), nil)
	if err != nil {
		return err
	}
	go func() {
		for {
			select {
			case nws := <-nwPairs:
				c.Lock()
				tmpview := networkTable{}
				lview := c.networks
				c.Unlock()
				for k, v := range lview {
					if v.isGlobalScoped() {
						tmpview[k] = v
					}
				}
				c.processNetworkUpdate(nws, &tmpview)

				// Delete processing
				for k := range tmpview {
					c.Lock()
					existing, ok := c.networks[k]
					c.Unlock()
					if !ok {
						continue
					}
					tmp := network{}
					if err := c.globalStore.GetObject(datastore.Key(existing.Key()...), &tmp); err != datastore.ErrKeyNotFound {
						continue
					}
					if err := existing.deleteNetwork(); err != nil {
						log.Debugf("Delete failed %s: %s", existing.name, err)
					}
				}
			}
		}
	}()
	return nil
}

func (n *network) watchEndpoints() error {
	if !n.ctrlr.validateGlobalStoreConfig() {
		return nil
	}

	n.Lock()
	cs := n.ctrlr.globalStore
	tmp := endpoint{network: n}
	n.stopWatchCh = make(chan struct{})
	stopCh := n.stopWatchCh
	n.Unlock()

	epPairs, err := cs.KVStore().WatchTree(datastore.Key(tmp.KeyPrefix()...), stopCh)
	if err != nil {
		return err
	}
	go func() {
		for {
			select {
			case <-stopCh:
				return
			case eps := <-epPairs:
				n.Lock()
				tmpview := endpointTable{}
				lview := n.endpoints
				n.Unlock()
				for k, v := range lview {
					if v.network.isGlobalScoped() {
						tmpview[k] = v
					}
				}
				n.ctrlr.processEndpointsUpdate(eps, &tmpview)
				// Delete processing
				for k := range tmpview {
					n.Lock()
					existing, ok := n.endpoints[k]
					n.Unlock()
					if !ok {
						continue
					}
					tmp := endpoint{}
					if err := cs.GetObject(datastore.Key(existing.Key()...), &tmp); err != datastore.ErrKeyNotFound {
						continue
					}
					if err := existing.deleteEndpoint(); err != nil {
						log.Debugf("Delete failed %s: %s", existing.name, err)
					}
				}
			}
		}
	}()
	return nil
}

func (n *network) stopWatch() {
	n.Lock()
	if n.stopWatchCh != nil {
		close(n.stopWatchCh)
		n.stopWatchCh = nil
	}
	n.Unlock()
}

func (c *controller) processNetworkUpdate(nws []*store.KVPair, prune *networkTable) {
	for _, kve := range nws {
		var n network
		err := json.Unmarshal(kve.Value, &n)
		if err != nil {
			log.Error(err)
			continue
		}
		if prune != nil {
			delete(*prune, n.id)
		}
		n.SetIndex(kve.LastIndex)
		c.Lock()
		existing, ok := c.networks[n.id]
		c.Unlock()
		if ok {
			existing.Lock()
			// Skip existing network update
			if existing.dbIndex != n.Index() {
				// Can't use SetIndex() since existing is locked.
				existing.dbIndex = n.Index()
				existing.dbExists = true
				existing.endpointCnt = n.endpointCnt
			}
			existing.Unlock()
			continue
		}

		if err = c.newNetworkFromStore(&n); err != nil {
			log.Error(err)
		}
	}
}

func (c *controller) processEndpointUpdate(ep *endpoint) bool {
	nw := ep.network
	if nw == nil {
		return true
	}
	nw.Lock()
	id := nw.id
	nw.Unlock()

	c.Lock()
	n, ok := c.networks[id]
	c.Unlock()
	if !ok {
		return true
	}
	existing, _ := n.EndpointByID(ep.id)
	if existing == nil {
		return true
	}

	ee := existing.(*endpoint)
	ee.Lock()
	if ee.dbIndex != ep.Index() {
		// Can't use SetIndex() because ee is locked.
		ee.dbIndex = ep.Index()
		ee.dbExists = true
		ee.sandboxID = ep.sandboxID
	}
	ee.Unlock()

	return false
}

func (c *controller) getLocalStoreConfig(cfg *config.Config) *config.DatastoreCfg {
	if cfg != nil && cfg.LocalStore.Client.Provider != "" && cfg.LocalStore.Client.Address != "" {
		return &cfg.LocalStore
	}
	return &defaultLocalStoreConfig
}

func (c *controller) getNetworksFromLocalStore() ([]*store.KVPair, error) {
	c.Lock()
	cs := c.localStore
	c.Unlock()
	return cs.KVStore().List(datastore.Key(datastore.NetworkKeyPrefix))
}

func (c *controller) getDataStore(dataScope datastore.DataScope) (dataStore datastore.DataStore) {
	c.Lock()
	if dataScope == datastore.GlobalScope {
		dataStore = c.globalStore
	} else if dataScope == datastore.LocalScope {
		dataStore = c.localStore
	}
	c.Unlock()
	return
}

func (c *controller) getEndpointsFromLocalStore() ([]*store.KVPair, error) {
	c.Lock()
	cs := c.localStore
	c.Unlock()
	return cs.KVStore().List(datastore.Key(datastore.EndpointKeyPrefix))
}

func (c *controller) processEndpointsUpdate(eps []*store.KVPair, prune *endpointTable) {
	for _, epe := range eps {
		var ep endpoint
		err := json.Unmarshal(epe.Value, &ep)
		if err != nil {
			log.Error(err)
			continue
		}
		if prune != nil {
			delete(*prune, ep.id)
		}
		ep.SetIndex(epe.LastIndex)
		if nid, err := ep.networkIDFromKey(epe.Key); err != nil {
			log.Error(err)
			continue
		} else {
			if n, err := c.NetworkByID(nid); err != nil {
				log.Error(err)
				continue
			} else {
				ep.network = n.(*network)
			}
		}
		if c.processEndpointUpdate(&ep) {
			err = c.newEndpointFromStore(epe.Key, &ep)
			if err != nil {
				log.Error(err)
			}
		}
	}
}
