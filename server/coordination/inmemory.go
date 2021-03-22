package coordination

import (
	"fmt"
	"github.com/jitsucom/jitsu/server/storages"
	"sync"
	"sync/atomic"
	"time"
)

type InMemoryLock struct {
	identifier string
}

func (iml *InMemoryLock) Unlock() {
}

func (iml *InMemoryLock) Identifier() string {
	return iml.identifier
}

//InMemoryService implementation for Service
type InMemoryService struct {
	serverNameSingleArray []string

	//table versions
	systemCollectionVersions map[string]*int64
	versionMutex             sync.RWMutex

	//for locking in single en node setup
	locks *sync.Map
}

func NewInMemoryService(serverNameSingleArray []string) *InMemoryService {
	return &InMemoryService{
		serverNameSingleArray:    serverNameSingleArray,
		systemCollectionVersions: map[string]*int64{},
		locks:                    &sync.Map{},
	}
}

func (ims *InMemoryService) GetInstances() ([]string, error) {
	return ims.serverNameSingleArray, nil
}

//Lock try to get a lock and wait 5 seconds if failed
func (ims *InMemoryService) Lock(system, collection string) (storages.Lock, error) {
	return ims.lockWithRetry(system, collection, 0)
}

func (ims *InMemoryService) TryLock(system string, collection string) (storages.Lock, error) {
	return ims.lockWithRetry(system, collection, 3)
}

func (ims *InMemoryService) Unlock(lock storages.Lock) error {
	ims.locks.Delete(lock.Identifier())
	return nil
}

func (ims *InMemoryService) IsLocked(system string, collection string) (bool, error) {
	identifier := getIdentifier(system, collection)
	_, locked := ims.locks.Load(identifier)
	return locked, nil
}

func (ims *InMemoryService) GetVersion(system string, collection string) (int64, error) {
	ims.versionMutex.RLock()
	defer ims.versionMutex.RUnlock()

	identifier := getIdentifier(system, collection)
	version, ok := ims.systemCollectionVersions[identifier]
	if !ok {
		return 0, nil
	}

	result := atomic.LoadInt64(version)
	return result, nil
}

func (ims *InMemoryService) IncrementVersion(system string, collection string) (int64, error) {
	ims.versionMutex.Lock()
	defer ims.versionMutex.Unlock()

	identifier := getIdentifier(system, collection)
	version, ok := ims.systemCollectionVersions[identifier]
	if !ok {
		var v int64
		version = &v
		ims.systemCollectionVersions[identifier] = version
	}

	result := atomic.AddInt64(version, 1)
	return result, nil
}

func (ims *InMemoryService) Close() error {
	return nil
}

//try to get a lock 3 times with every time 1 second delay
func (ims *InMemoryService) lockWithRetry(system, collection string, retryCount int) (storages.Lock, error) {
	identifier := getIdentifier(system, collection)
	_, loaded := ims.locks.LoadOrStore(identifier, true)
	if loaded {
		if retryCount >= 3 {
			return nil, fmt.Errorf("Error in-memory locking [%s] system [%s] collection: already locked", system, collection)
		}

		time.Sleep(time.Second)
		return ims.lockWithRetry(system, collection, retryCount+1)
	}

	return &InMemoryLock{identifier: identifier}, nil
}

func getIdentifier(system, collection string) string {
	return system + "_" + collection
}
