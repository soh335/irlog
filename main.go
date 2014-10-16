package main

import (
	"database/sql"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	_ "github.com/mattn/go-sqlite3"
)

var (
	clientkey = flag.String("clientkey", "", "clientkey")
	db        = flag.String("db", "irlog.db", "db")
	host      = flag.String("host", "0.0.0.0", "host")
	port      = flag.String("port", "3355", "port")
	setup     = flag.Bool("setup", false, "setup")
	agent     = flag.Bool("agent", true, "agent")
)

func main() {
	flag.Parse()

	db, err := _db()

	if err != nil {
		log.Fatal(err)
	}

	defer db.Close()

	if *setup {
		if err := _setup(db); err != nil {
			log.Fatal(err)
		}
	} else {
		c := make(chan error, 2)
		stop := make(chan struct{})
		var wg sync.WaitGroup

		s := make(chan os.Signal)
		signal.Notify(s, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			n := <-s
			log.Println("got signal:", n)
			stop <- struct{}{}
		}()

		a := &Agent{}
		a.ClientKey = *clientkey
		a.Stop = stop

		if *agent {
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := a.Run(db)
				if err != nil {
					log.Println(err)
				}
				c <- err
			}()
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			err := _web(net.JoinHostPort(*host, *port), db, a, stop)
			if err != nil {
				log.Println(err)
			}
			c <- err
		}()

		<-c
		close(stop)
		wg.Wait()
	}
}

func _db() (*sql.DB, error) {
	db, err := sql.Open("sqlite3", *db)
	if err != nil {
		return nil, err
	}
	return db, nil
}

func _setup(db *sql.DB) error {

	stmt := `
	drop table if exists irlog;
	create table irlog (
		id integer not null primary key autoincrement,
		format text,
		freq integer,
		data text,
		data_hash text,
		hostname text,
		deviceid text,
		created_at integer
	);

	drop table if exists data_name;
	create table data_name (
		id integer not null primary key autoincrement,
		data_hash text,
		name text
	);
	create unique index uniq_data_name_data_hash on data_name(data_hash);
	`

	if _, err := db.Exec(stmt); err != nil {
		return err
	}

	return nil
}
