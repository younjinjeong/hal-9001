package hal

import (
	"log"
	"math/rand"
	"sync"
	"time"
)

type PeriodicFunc struct {
	Name     string
	Interval time.Duration
	Function func()
	NoRand   bool // set to true to disable randomizing the first execution
	last     time.Time
	status   string
	running  bool
	tick     <-chan time.Time
	run      chan time.Time
	exit     chan struct{}
	starting sync.WaitGroup
	stopping sync.WaitGroup
	init     sync.Once
	mut      sync.Mutex
}

var periodicData struct {
	funcs []*PeriodicFunc
	mut   sync.Mutex
}

func init() {
	periodicData.funcs = make([]*PeriodicFunc, 0)
}

// GetPeriodicFunc finds a periodic function by name and returns a pointer to it.
// If the name is not found, nil is returned.
func GetPeriodicFunc(name string) *PeriodicFunc {
	periodicData.mut.Lock()
	defer periodicData.mut.Unlock()

	for _, pf := range periodicData.funcs {
		if pf.Name == name {
			return pf
		}
	}

	return nil
}

// initialize internal fields, called automatically using pf.init.Do
func (pf *PeriodicFunc) initialize() {
	periodicData.mut.Lock()
	defer periodicData.mut.Unlock()

	pf.status = "initialized"
	pf.exit = make(chan struct{})
	pf.tick = make(<-chan time.Time)
	pf.run = make(chan time.Time)
}

// loop is the goroutine's program loop
func (pf *PeriodicFunc) loop() {
	pf.mut.Lock()
	pf.status = "running"
	pf.running = true
	pf.mut.Unlock()

	pf.starting.Done()

	// TODO: this should capture/handle panics like router.go does
pfLoop:
	for {
		select {
		case <-pf.exit:
			pf.status = "stopped"
			break pfLoop
		case t := <-pf.tick:
			log.Printf("PeriodicFunc tick %q @ %s", pf.Name, t)
			pf.runFunc(t)
		case t := <-pf.run:
			log.Printf("PeriodicFunc run %q @ %s", pf.Name, t)
			pf.runFunc(t)
		}
	}

	pf.mut.Lock()
	pf.running = false
	pf.mut.Unlock()

	pf.stopping.Done()
}

// runFunc runs the provided function while holding the pf's mutex.
func (pf *PeriodicFunc) runFunc(t time.Time) {
	pf.mut.Lock()
	defer pf.mut.Unlock()

	pf.last = t
	pf.Function()
}

// Register puts a pf in the global list and makes it available to GetPeriodicFunc.
// Anonymous pf's work fine but are not retreivable.
func (pf *PeriodicFunc) Register() {
	found := GetPeriodicFunc(pf.Name)
	if found != nil {
		log.Printf("Found duplicate name %q in list of PeriodicFuncs while registering.", pf.Name)
	}

	periodicData.mut.Lock()
	defer periodicData.mut.Unlock()

	periodicData.funcs = append(periodicData.funcs, pf)
}

// Start a periodic function.
func (pf *PeriodicFunc) Start() {
	pf.init.Do(pf.initialize)

	pf.mut.Lock()

	pf.tick = time.Tick(pf.Interval)
	pf.starting.Add(1)

	go func() {
		// avoid a thundering herd by sleeping for a random number of seconds
		if !pf.NoRand {
			pf.randSleep()
		}

		pf.loop() // may block on pf.mut until Unlock()
	}()

	pf.mut.Unlock()

	pf.starting.Wait() // wait for the goroutine to call Done()

	// run the first pass immediately
	pf.run <- time.Now()
}

// Stop a periodic function.
func (pf *PeriodicFunc) Stop() {
	pf.init.Do(pf.initialize)
	pf.mut.Lock()
	defer pf.mut.Unlock()

	pf.exit <- struct{}{}
}

// Bump schedules a periodic function to update outside of the scheduled times.
// The value of pf.Last() is updated when this is used.
func (pf *PeriodicFunc) Bump() {
	pf.init.Do(pf.initialize)
	pf.mut.Lock()
	defer pf.mut.Unlock()

	pf.run <- time.Now()
}

// Status returns initialized/running/stopped state as a string.
func (pf *PeriodicFunc) Status() string {
	pf.init.Do(pf.initialize)
	pf.mut.Lock()
	defer pf.mut.Unlock()

	return pf.status
}

// Last returns the wallclock time of the last run of the function.
func (pf *PeriodicFunc) Last() time.Time {
	pf.init.Do(pf.initialize)
	pf.mut.Lock()
	defer pf.mut.Unlock()

	return pf.last
}

// randSleep selects a random number between 0 and 60 and sleeps that many
// seconds before returning. While sleeping, the pf status is set to "sleeping".
func (pf *PeriodicFunc) randSleep() {
	pf.mut.Lock()
	pf.status = "sleeping"
	pf.mut.Unlock()

	randSecs := rand.Intn(60)
	time.Sleep(time.Second * time.Duration(randSecs))
}
