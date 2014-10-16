package main

import (
	"crypto/md5"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Response struct {
	Message  Message `json:"message"`
	Hostname string  `json:"hostname"`
	Deviceid string  `json:"deviceid"`
}

type Message struct {
	Format string `json:"format"`
	Freq   int    `json:"freq"`
	Data   []int  `json:"data"`
}

func (r *Response) DataString() string {
	s := []string{}
	for _, i := range r.Message.Data {
		s = append(s, strconv.Itoa(i))
	}
	return strings.Join(s, ",")
}

func (r *Response) DataMd5() string {
	m := md5.New()
	m.Write([]byte(r.DataString()))
	return fmt.Sprintf("%x", m.Sum(nil))
}

type Agent struct {
	ClientKey string
	Stop      chan struct{}
}

func (a *Agent) Run(db *sql.DB) error {
	for {
		if err := a.Get(db); err != nil {
			return err
		}
	}

	panic("not reach")
}

func (a *Agent) Get(db *sql.DB) error {

	u, err := url.Parse("https://api.getirkit.com/1/messages")

	if err != nil {
		return err
	}

	q := u.Query()
	q.Set("clientkey", a.ClientKey)
	q.Set("clear", "1")

	u.RawQuery = q.Encode()

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return err
	}

	log.Println("request", req)

	var r Response
	tr := &http.Transport{}
	client := &http.Client{Transport: tr}
	c := make(chan error)
	empty := false

	f := func(req *http.Request) error {
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		if res.StatusCode != 200 {
			return fmt.Errorf("failed: %s", res.Status)
		}

		if res.ContentLength == 0 {
			empty = true
			return nil
		}

		defer res.Body.Close()

		if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
			return err
		}
		return nil
	}

	go func() { c <- f(req) }()

	select {
	case <-a.Stop:
		tr.CancelRequest(req)
		err := <-c
		return err
	case <-time.After(time.Second * 120): // for detect timeout err
		log.Println("timeout")
		tr.CancelRequest(req)
		<-c
		return nil
	case err := <-c:
		if err != nil {
			return err
		}
	}

	if empty {
		return nil
	}

	stmt := `insert into irlog(format, freq, data, data_hash, hostname, deviceid, created_at) values(?, ?, ?, ?, ?, ?, ?)`

	if _, err := db.Exec(stmt, r.Message.Format, r.Message.Freq, r.DataString(), r.DataMd5(), r.Hostname, r.Deviceid, (time.Now().UnixNano() / int64(time.Millisecond))); err != nil {
		return err
	}

	log.Println("insert", r)

	return nil
}

func (a *Agent) Post(deviceid string, msg *Message) error {
	u, err := url.Parse("https://api.getirkit.com/1/messages")
	if err != nil {
		return err
	}

	q := u.Query()
	q.Set("clientkey", a.ClientKey)
	q.Set("deviceid", deviceid)

	byt, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	q.Set("message", string(byt))
	res, err := http.PostForm(u.String(), q)
	if err != nil {
		return err
	}

	if res.StatusCode != 200 {
		return fmt.Errorf(res.Status)
	}

	return nil
}
