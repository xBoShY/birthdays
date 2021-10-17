package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"regexp"
	"time"

	"cloud.google.com/go/civil"
	"github.com/gorilla/mux"
)

type Birthdays struct {
	Wait      func() (err error)   // always returns a non-nil error. After Close(), the returned error is ErrServerClosed.
	Close     func()               // Close the Birthdays
	GetListen func() (addr string) // Returns the listening address
}

type PutRequestParams struct {
	DateOfBirth string
}

type Message struct {
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// maxWorkers limits the number of concurrent requests
func (m *Metrics) maxWorkers(h http.Handler, n uint8, timeout uint64) http.Handler {
	// use a synchronous channel of size n to limit the number of parallel requests by n
	sema := make(chan struct{}, n)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// a request will either register (by sending to the channel) or timeout
		var timedout bool = false
		t1 := time.Now()
		select {
		case sema <- struct{}{}:
			// unlock the worker when processing completes
			defer func() { <-sema }()
		case <-time.After(time.Millisecond * time.Duration(timeout)):
			w.WriteHeader(http.StatusRequestTimeout)
			m.IncService(http.StatusRequestTimeout)
			timedout = true
		}

		// Calculate the wait time for an available worker
		t2 := time.Now()
		tWait := float64(int64(t2.Sub(t1) / time.Millisecond))
		if tWait > 0 {
			m.AddWorkerWait(tWait)
		}

		if timedout {
			return
		}

		h.ServeHTTP(w, r)
	})
}

func isLeapYear(year int) bool {
	return year%400 == 0 || (year%4 == 0 && year%100 != 0)
}

func (msg Message) String() (string, error) {
	bytes, err := json.Marshal(msg)
	if err != nil {
		return "", err
	}

	return string(bytes), nil
}

// requestHandler has all the request processing logic
func requestHandler(w http.ResponseWriter, r *http.Request, p PersistenceInterface) (string, int) {
	var err error

	// request vars
	var (
		username    string
		dateOfBirth civil.Date
		data        []byte // holds inbound data to be parsed and used
		reqParams   PutRequestParams
	)

	// response vars
	var (
		status  int
		content string
	)

	router_vars := mux.Vars(r)
	username = router_vars["username"]
	isOnlyLetters := regexp.MustCompile(`^[A-Za-z]+$`).MatchString
	if !isOnlyLetters(username) {
		content, err = Message{
			Error: "invalid username",
		}.String()
		if err != nil {
			return "", http.StatusInternalServerError
		}
		return content, http.StatusBadRequest
	}

	today := civil.DateOf(time.Now())

	// Implemented HTTP methods
	switch r.Method {
	case "PUT":
		data, err = ioutil.ReadAll(r.Body)
		if err != nil {
			content, err = Message{
				Error: "unable to read input",
			}.String()
			if err != nil {
				return "", http.StatusInternalServerError
			}
			return content, http.StatusInternalServerError
		}

		// Parsing JSON to reqParams
		json.Unmarshal(data, &reqParams)
		if err != nil {
			content, err = Message{
				Error: "couldnt parse the message",
			}.String()
			if err != nil {
				return "", http.StatusInternalServerError
			}
			return content, http.StatusInternalServerError
		}

		dateOfBirth, err = civil.ParseDate(reqParams.DateOfBirth)
		if err != nil || !dateOfBirth.Before(today) {
			content, err = Message{
				Error: "invalid date",
			}.String()
			if err != nil {
				return "", http.StatusInternalServerError
			}
			return content, http.StatusBadRequest
		}

		err = p.Put(username, dateOfBirth)
		if err != nil {
			content, err = Message{
				Error: err.Error(),
			}.String()
			if err != nil {
				return "", http.StatusInternalServerError
			}
			return content, http.StatusInternalServerError
		}

		content = ""
		status = http.StatusNoContent

	case "GET":
		dateOfBirth, err = p.Get(username)
		if err != nil {
			content, err = Message{
				Error: err.Error(),
			}.String()
			if err != nil {
				return "", http.StatusInternalServerError
			}
			return content, http.StatusBadRequest
		}

		birthday := dateOfBirth
		birthday.Year = today.Year
		if birthday.Day == 29 && birthday.Month == 2 {
			if today.Before(civil.Date{Year: today.Year, Month: 3, Day: 1}) {
				if !isLeapYear(birthday.Year) {
					birthday.Month = 3
					birthday.Day = 1
				}
			} else {
				birthday.Year = birthday.Year + 1
				if !isLeapYear(birthday.Year) {
					birthday.Month = 3
					birthday.Day = 1
				}
			}
		}

		days := today.DaysSince(birthday)
		if days > 0 {
			birthday.Year = birthday.Year + 1
			days = today.DaysSince(birthday)
		}
		days = days * -1
		if days == 0 {
			content, err = Message{
				Message: fmt.Sprintf("Hello, %s! Happy birthday!", username),
			}.String()
			if err != nil {
				return "", http.StatusInternalServerError
			}
		} else {
			content, err = Message{
				Message: fmt.Sprintf("Hello, %s! Your birthday is in %d day(s)", username, days),
			}.String()
			if err != nil {
				return "", http.StatusInternalServerError
			}
		}
		status = http.StatusOK

	default:
		content = ""
		status = http.StatusNotImplemented
	}

	return content, status
}

type handlerExtra struct {
	m *Metrics
	p PersistenceInterface
}

// requestHandlerWrapper registers the metrics logic
func (he handlerExtra) requestHandlerWrapper(w http.ResponseWriter, r *http.Request) {
	p := he.p
	m := he.m

	content, status := requestHandler(w, r, p)

	w.WriteHeader(status)
	w.Write([]byte(content))

	m.IncService(status)
}

// NewBirthdays generates a Birthdays, which will serve the requests on address <listen>
func (m *Metrics) NewBirthdays(endpoint string, listen string, workers uint8, timeout uint64, p PersistenceInterface) (*Birthdays, error) {
	var he handlerExtra
	he.m = m
	he.p = p

	handler := http.HandlerFunc(he.requestHandlerWrapper)

	httpListener, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, err
	}

	r := mux.NewRouter()
	r.Handle(endpoint, m.maxWorkers(handler, workers, timeout))

	httpSrv := &http.Server{
		Handler: r,
	}

	//This channel will receive the error generated by http.Server.Serve
	//Its useful for the blocking funcion NewBirthdays.Wait()
	ch := make(chan error)
	go func() {
		err := httpSrv.Serve(httpListener)
		ch <- err
	}()

	b := new(Birthdays)
	b.Wait = func() error {
		return <-ch
	}

	b.Close = func() {
		httpSrv.Shutdown(context.Background())
		httpListener.Close()
	}

	b.GetListen = func() string {
		return httpListener.Addr().String()
	}

	return b, nil
}
