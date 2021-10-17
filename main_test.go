package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/civil"
	"github.com/google/uuid"
	"go.etcd.io/etcd/embed"
)

type Persistence_Test struct {
	errors               <-chan error
	endpoints            string
	PersistenceInterface PersistenceInterface
	Close                func()
}

var (
	args = Arguments{
		Listen:        ":0",
		Workers:       2,
		Timeout:       1000,
		MetricsListen: ":0",
	}

	persist *Persistence_Test = nil
	svc     *Services         = nil
)

func getPersistencePlugin_etcd() (p *Persistence_Test, err error) {
	id := uuid.New()
	cfg := embed.NewConfig()
	cfg.ForceNewCluster = true
	cfg.Dir = fmt.Sprintf("./_test/test.etcd/%s", id.String())
	e, err := embed.StartEtcd(cfg)
	if err != nil {
		return nil, err
	}
	select {
	case <-e.Server.ReadyNotify():
		log.Printf("Server is ready!")
	case <-time.After(60 * time.Second):
		e.Server.Stop()
		log.Printf("Server took too long to start!")
	}

	p = new(Persistence_Test)
	p.errors = e.Err()

	p.Close = func() {
		defer e.Close()
		e.Server.Stop()
	}
	p.endpoints = "127.0.0.1:2379"
	options := fmt.Sprintf("{ \"endpoints\": \"%s\", \"path\": \"/users\" }", p.endpoints)
	p.PersistenceInterface, err = NewPersistenceEtcd(options)

	return p, err
}

func (svc *Services) put(username string, dateOfBirth string) (int, error) {
	log.Printf("Calling PUT(%s, %s)", username, dateOfBirth)
	body := PutRequestParams{
		DateOfBirth: dateOfBirth,
	}
	json, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}

	client := &http.Client{}

	req, err := http.NewRequest(http.MethodPut, "http://"+svc.birthdays.GetListen()+"/hello/"+username, bytes.NewBuffer(json))
	if err != nil {
		return 0, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}

	log.Printf("PUT(%s, %s) result: %d", username, dateOfBirth, resp.StatusCode)
	return resp.StatusCode, nil
}

func (svc *Services) get(username string) (string, int, error) {
	log.Printf("Calling GET(%s)", username)
	client := &http.Client{}
	req, err := http.NewRequest(http.MethodGet, "http://"+svc.birthdays.GetListen()+"/hello/"+username, nil)
	if err != nil {
		return "", 0, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, err
	}
	bodystr := string(body)

	log.Printf("GET(%s) result: %d - %s", username, resp.StatusCode, bodystr)
	return bodystr, resp.StatusCode, nil
}

func tryGet(t *testing.T, username string, expectedStatus int, testDateStr string) {
	msgStr, status, err := svc.get(username)
	if err != nil {
		log.Fatalf(err.Error())
	}

	if status != expectedStatus {
		t.Fatalf("Expected Status Code %d, got %d", expectedStatus, status)
	}

	if status == 200 {
		today := civil.DateOf(time.Now())
		testDate, err := civil.ParseDate(testDateStr)
		if err != nil {
			log.Fatalf(err.Error())
		}

		var msg Message
		err = json.Unmarshal([]byte(msgStr), &msg)
		if err != nil {
			log.Fatalf(err.Error())
		}

		testDate.Year = today.Year
		if testDate.Before(today) {
			testDate.Year = today.Year + 1
		}

		if !isLeapYear(today.Year) && testDate.Month == 2 && testDate.Day == 29 {
			testDate.Month = 3
			testDate.Day = 1
		}

		if msg.Message == fmt.Sprintf("Hello, %s! Happy birthday!", username) {
			if !(testDate.Month == today.Month && testDate.Day == today.Day) {
				log.Fatalf("Wrong message.")
			}
		} else {
			idx := strings.Index(msgStr, "Your birthday is in")
			substr := msgStr[idx:]
			words := strings.Fields(substr)
			daysStr := words[4]
			days, err := strconv.Atoi(daysStr)
			if err != nil {
				log.Fatalf(err.Error())
			}

			birthday := today.AddDays(days)
			if !(testDate.Month == birthday.Month && testDate.Day == birthday.Day) {
				if !(testDate.Month == today.Month && testDate.Day == today.Day) {
					log.Fatalf("Wrong message.")
				}
			}
		}
	}
}

func TestGetNotExists(t *testing.T) {
	tryGet(t, "revolut", 400, "")
}

func tryPut(t *testing.T, username string, dateOfBirth string, expectedStatus int) {
	status, err := svc.put(username, dateOfBirth)
	if err != nil {
		log.Fatalf(err.Error())
	}

	if status != expectedStatus {
		t.Fatalf("Expected Status Code %d, got %d", expectedStatus, status)
	}
}

func TestPut(t *testing.T) {
	tryPut(t, "revolut", "2015-07-01", 204)
}

func TestInvalidUsername(t *testing.T) {
	tryPut(t, "abc1", "2000-01-01", 400)
	tryGet(t, "abc1", 400, "2000-01-01")

	tryPut(t, "abc-1", "2000-01-01", 400)
	tryGet(t, "abc-1", 400, "2000-01-01")
}

func TestInvalidDates(t *testing.T) {
	tryPut(t, "revolut", "meneses", 400)
	tryPut(t, "revolut", "2000-04-31", 400)
	tryPut(t, "revolut", "2015-02-29", 400)
}

func tryDate(t *testing.T, username string, dateOfBirth civil.Date) {
	tryPut(t, username, dateOfBirth.String(), 204)
	tryGet(t, username, 200, dateOfBirth.String())
}

func TestGetExists(t *testing.T) {
	dateOfBirth, err := civil.ParseDate("2015-07-01")
	if err != nil {
		log.Fatalf(err.Error())
	}

	tryDate(t, "revolut", dateOfBirth)
}

func TestUsernameCasing(t *testing.T) {
	dateOfBirth, err := civil.ParseDate("2015-07-01")
	if err != nil {
		log.Fatalf(err.Error())
	}

	tryDate(t, "ReVoLuT", dateOfBirth)
}

func TestLeapDates(t *testing.T) {
	year := 1800
	endYear := time.Now().Year()
	for year < endYear {
		if isLeapYear(year) {
			dateOfBirth := civil.Date{Year: year, Month: 02, Day: 29}
			log.Printf("Testing date: %s", dateOfBirth.String())
			tryDate(t, "revolut", dateOfBirth)
		}

		year = year + 4
	}
}

func TestBirthDates(t *testing.T) {
	dateOfBirth := civil.Date{Year: 1999, Month: 12, Day: 31}
	dateEnd := civil.Date{Year: 2001, Month: 01, Day: 01}

	for dateOfBirth.Before(dateEnd) {
		log.Printf("Testing date: %s", dateOfBirth.String())
		tryDate(t, "revolut", dateOfBirth)

		dateOfBirth = dateOfBirth.AddDays(5)
	}
}

func TestBirthdayToday(t *testing.T) {
	dateOfBirth := civil.DateOf(time.Now())
	dateOfBirth.Year = dateOfBirth.Year - 1
	tryDate(t, "Today", dateOfBirth)
}

func TestMain(m *testing.M) {
	var err error

	fmt.Println("=== INIT  etcd")
	start := time.Now()
	persist, err = getPersistencePlugin_etcd()
	if err != nil {
		log.Fatalf(err.Error())
	}

	time.Sleep(1 * time.Second)

	go func() {
		log.Fatal(<-persist.errors)
	}()

	defer persist.Close()
	dur := time.Since(start)
	z := time.Unix(0, 0).UTC()
	fmt.Printf("--- PASS: etcd (%ss)\n", z.Add(time.Duration(dur)).Format("5.99"))

	svc = StartService(args, persist.PersistenceInterface)
	defer svc.birthdays.Close()
	defer svc.metrics.Close()

	code := m.Run()
	os.Exit(code)
}
