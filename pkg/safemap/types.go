package safemap

type DataStore interface {
	Get(key string, op func(value interface{}) error) (err error, found bool)
	Set(key string, value interface{})
	Delete(key string)
	Len() int
	Dump(cloner func(value interface{}) interface{}) map[string]interface{}
}

type SafeMap struct {
	store      map[string]interface{}
	requestsCh chan chan interface{}
	closeCh    chan interface{}
	closed     bool
}
