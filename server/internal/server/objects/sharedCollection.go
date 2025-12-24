package objects

import (
	"sync"
)

type SharedCollection[T any] struct {
	objectsMap map[uint64]T
	nextId     uint64
	mapMux     sync.Mutex
}

func NewSharedCollection[T any](capacity ...int) *SharedCollection[T] {
	var newObjMap map[uint64]T

	if len(capacity) > 0 {
		newObjMap = make(map[uint64]T, capacity[0])
	} else {
		newObjMap = make(map[uint64]T)
	}

	return &SharedCollection[T]{
		objectsMap: newObjMap,
		nextId: 1,
	}
}

// Add an object to the map with the given ID (if provided) or the next available ID.
// Returns the ID of the object added
func (collection *SharedCollection[T]) Add(obj T, id ...uint64) uint64 {
	collection.mapMux.Lock()
	defer collection.mapMux.Unlock()

	thisId := collection.nextId

	if len(id) > 0 {
		thisId = id[0]
	}

	collection.objectsMap[thisId] = obj
	collection.nextId++

	return thisId
}

// Removes an object from the map by ID, if it exists
func (collection *SharedCollection[T]) Remove(id uint64) {
	collection.mapMux.Lock()
	defer collection.mapMux.Unlock()

	delete(collection.objectsMap, id)
}

//  Call the callback function for each object in the map
func (collection *SharedCollection[T]) ForEach(callback func(uint64, T)) {
	// Create a local copy while holding the lock
	collection.mapMux.Lock()
	localCopy := make(map[uint64]T, len(collection.objectsMap))

	for id, obj := range collection.objectsMap {
		localCopy[id] = obj
	}

	collection.mapMux.Unlock()

	// Iterate over the local copy without holding the lock
	for id, obj := range localCopy {
		callback(id, obj)
	}
}

// Get an object with the given ID, if it exists, otherwise nil
// Also returns a boolean indicating whether the object was found
func (collection *SharedCollection[T]) Get(id uint64) (T, bool) {
	collection.mapMux.Lock()
	defer collection.mapMux.Unlock()

	obj, found := collection.objectsMap[id]

	return obj, found
}

// Get the approximate number of objects in the map
// The reason this is approximate is because the map is read without holding the lock
func (collection *SharedCollection[T]) len() int {
	return len(collection.objectsMap)
}