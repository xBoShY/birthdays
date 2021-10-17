package main

import (
	"log"

	"github.com/cosiner/flag"
)

// Arguments defines the available app arguments
type Arguments struct {
	Listen             string `names:"--listen" env:"BIRTHDAYS_Listen" usage:"Service listen address." default:":8080"`
	Workers            uint8  `names:"--workers" env:"BIRTHDAYS_Workers" usage:"Number of serving workers." default:"2"`
	Timeout            uint64 `names:"--timeout" env:"BIRTHDAYS_Timeout" usage:"Maximum time (in milliseconds) to wait for a worker." default:"1000"`
	MetricsListen      string `names:"--metrics" env:"BIRTHDAYS_Metrics" usage:"Metrics listen address." default:":9095"`
	PersistenceOptions string `names:"--persistence-options" env:"BIRTHDAYS_PersistenceOptions" usage:"Persistence options" default:"{ \"endpoints\": \"localhost:2379\", \"path\": \"/users\" }"`
}

// Service contains the Metrics logic and the Birthdays logic
type Services struct {
	metrics     *Metrics
	birthdays   *Birthdays
	persistence PersistenceInterface
}

// StartService generates and start a Service
func StartService(args Arguments, pi PersistenceInterface) *Services {
	m, err := NewMetrics("/metrics", args.MetricsListen)
	if err != nil {
		log.Fatal(err)
	}

	b, err := m.NewBirthdays("/hello/{username}", args.Listen, args.Workers, args.Timeout, pi)
	if err != nil {
		log.Fatal(err)
	}

	svc := new(Services)
	svc.metrics = m
	svc.birthdays = b
	svc.persistence = pi

	return svc
}

func main() {
	var args Arguments
	flag.Commandline.ParseStruct(&args)

	pi, err := NewPersistence(args.PersistenceOptions)
	if err != nil {
		log.Fatal(err)
	}
	service := StartService(args, pi)

	defer service.persistence.Close()
	defer service.metrics.Close()
	log.Fatal(service.birthdays.Wait())
}
