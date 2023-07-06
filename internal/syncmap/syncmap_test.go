package syncmap_test

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"code.cloudfoundry.org/volumedriver/internal/syncmap"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SyncMap", func() {
	It("can put and get concurrently", func() {
		s := syncmap.New[int]()

		const workers = 100
		var wg sync.WaitGroup
		wg.Add(workers)
		for i := 0; i < workers; i++ {
			go func(workerID int) {
				defer GinkgoRecover()
				defer wg.Done()

				key := fmt.Sprintf("%d", workerID)
				s.Put(key, workerID)
				time.Sleep(100 * time.Millisecond)
				value, _ := s.Get(key)
				Expect(value).To(Equal(workerID))
			}(i)
		}

		wg.Wait()
	})

	It("can report whether a value exists", func() {
		s := syncmap.New[int]()
		s.Put("exists", 42)

		v1, ok1 := s.Get("exists")
		Expect(v1).To(Equal(42))
		Expect(ok1).To(BeTrue())

		v2, ok2 := s.Get("doesn't exist")
		Expect(v2).To(Equal(0))
		Expect(ok2).To(BeFalse())
	})

	It("can delete a value", func() {
		const key = "exists"
		s := syncmap.New[int]()
		s.Put(key, 42)
		_, ok1 := s.Get(key)
		Expect(ok1).To(BeTrue())

		s.Delete(key)
		_, ok2 := s.Get(key)
		Expect(ok2).To(BeFalse())
	})

	It("can be marshalled into JSON", func() {
		s := syncmap.New[any]()
		s.Put("foo", "bar")
		s.Put("baz", 42)
		Expect(json.Marshal(s)).To(MatchJSON(`{"foo":"bar","baz":42}`))
	})

	It("can be unmarshalled from JSON", func() {
		const input = `{"foo":"bar","baz":42}`
		s := syncmap.New[any]()

		Expect(json.Unmarshal([]byte(input), s)).To(Succeed())
		Expect(json.Marshal(s)).To(MatchJSON(input))
	})

	It("can return a list of keys", func() {
		s := syncmap.New[any]()
		s.Put("foo", "bar")
		s.Put("baz", 42)
		s.Put("quz", false)

		Expect(s.Keys()).To(ConsistOf("foo", "baz", "quz"))
	})

	It("can return a list of values", func() {
		s := syncmap.New[string]()
		s.Put("foo", "bar")
		s.Put("baz", "quz")
		s.Put("duz", "fuz")

		Expect(s.Values()).To(ConsistOf("bar", "fuz", "quz"))
	})
})
