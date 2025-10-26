package safemap

import "sync"

type WalkFunc func(key string, value interface{}) (keepgoing bool, err error)

type DataStore interface {
	Get(key string, op func(value interface{}) error) (err error, found bool)
	Set(key string, value interface{})
	Delete(key string)
	Len() int
	Dump(cloner func(value interface{}) interface{}) map[string]interface{}
	Walk(walkFunc WalkFunc) error
}

type SafeMap struct {
	store      map[string]interface{}
	requestsCh chan func()
	closeCh    chan struct{}
	closed     bool
	// A sync.Mutex is used here only to protect the 'closed' flag
	// during the Close() operation to prevent a race condition on that specific field.
	closeMutex sync.Mutex
}
