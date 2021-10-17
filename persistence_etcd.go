package main

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"errors"

	"cloud.google.com/go/civil"
	"go.etcd.io/etcd/clientv3"
)

type Options struct {
	Endpoints string
	Path      string
}

type persistence struct {
	cli  *clientv3.Client
	KV   clientv3.KV
	path string
}

type PersistenceInterface interface {
	Put(username string, dateOfBirth civil.Date) (err error)
	Get(username string) (dateOfBirth civil.Date, err error)
	Close()
}

func (p *persistence) Put(username string, dateOfBirth civil.Date) error {
	_, err := p.KV.Put(context.TODO(), getNode(p.path, username), dateOfBirth.String())

	return err
}

func getNode(path string, username string) string {
	return path + "/" + username
}

func (p *persistence) Get(username string) (civil.Date, error) {
	getResp, err := p.KV.Get(context.TODO(), getNode(p.path, username))
	if err != nil {
		return civil.DateOf(time.Now()), err
	}
	if getResp.Count == 0 {
		return civil.DateOf(time.Now()), errors.New("user does not exist")
	}

	val := getResp.Kvs[0].Value
	dateOfBirth, err := civil.ParseDate(string(val))

	return dateOfBirth, err
}

func (p *persistence) Close() {
	p.cli.Close()
}

func NewPersistenceEtcd(options string) (PersistenceInterface, error) {
	var opts Options
	json.Unmarshal([]byte(options), &opts)

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   strings.Split(opts.Endpoints, ","),
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	var p = new(persistence)
	p.cli = cli
	p.KV = clientv3.NewKV(cli)

	return p, nil
}

func NewPersistence(options string) (PersistenceInterface, error) {
	return NewPersistenceEtcd(options)
}
