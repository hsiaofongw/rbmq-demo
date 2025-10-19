package safemap

func NewSafeMap() *SafeMap {
	sm := &SafeMap{
		store:      make(map[string]interface{}),
		requestsCh: make(chan chan interface{}),
	}

	go func() {
		for {
			req := make(chan interface{})

			select {
			case sm.requestsCh <- req:
				<-req
			case <-sm.closeCh:
				close(sm.requestsCh)
				sm.closed = true
				return
			}
		}
	}()

	return sm
}

func (sm *SafeMap) Get(key string, op func(value interface{}) error) (err error, found bool) {
	req := <-sm.requestsCh
	defer close(req)

	if v, ok := sm.store[key]; ok {
		return op(v), true
	}

	return nil, false
}

func (sm *SafeMap) Set(key string, value interface{}) {
	req := <-sm.requestsCh
	defer close(req)
	sm.store[key] = value
}

func (sm *SafeMap) Delete(key string) {
	req := <-sm.requestsCh
	defer close(req)
	delete(sm.store, key)
}

func (sm *SafeMap) Len() int {
	req := <-sm.requestsCh
	defer close(req)
	len := len(sm.store)
	return len
}

func (sm *SafeMap) Dump(cloner func(value interface{}) interface{}) map[string]interface{} {
	req := <-sm.requestsCh
	defer close(req)
	result := make(map[string]interface{})
	for k, v := range sm.store {
		result[k] = cloner(v)
	}
	return result
}

func (sm *SafeMap) Close() {
	if sm.closed {
		return
	}
	close(sm.closeCh)
}
