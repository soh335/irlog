package main

import (
	"bytes"
	"database/sql"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-martini/martini"
	"github.com/martini-contrib/render"
	"github.com/martini-contrib/staticbin"
)

type IRLog struct {
	Id        int    `json:"id"`
	Format    string `json:"format"`
	Freq      int    `json:"freq"`
	Name      string `json:"name"`
	Data      string `json:"data"`
	DataHash  string `json:"-"`
	Hostname  string `json:"hostname"`
	Deviceid  string `json:"deviceid"`
	CreatedAt int64  `json:"created_at"`
}

func (i *IRLog) Message() (*Message, error) {
	data := []int{}
	for _, s := range strings.Split(i.Data, ",") {
		v, err := strconv.Atoi(s)
		if err != nil {
			return nil, err
		}
		data = append(data, v)
	}

	return &Message{
		Format: i.Format,
		Freq:   i.Freq,
		Data:   data,
	}, nil
}

func (i *IRLog) BindFromScanWithName(s RowScanner) error {
	var id int
	var format string
	var freq int
	var data string
	var hostname string
	var deviceid string
	var createdAt int64
	var name sql.NullString

	if err := s.Scan(&id, &format, &freq, &data, &hostname, &deviceid, &createdAt, &name); err != nil {
		return err
	}

	if !name.Valid {
		splited := strings.SplitN(data, ",", 4)
		name.String = strings.Join(splited[0:int(math.Min(3, float64(len(splited))))], ",") + "..."
	}

	i.Id = id
	i.Format = format
	i.Freq = freq
	i.Data = data
	i.Name = name.String
	i.Hostname = hostname
	i.Deviceid = deviceid
	i.CreatedAt = createdAt

	return nil
}

type RowScanner interface {
	Scan(dest ...interface{}) error
}

type QueryRower interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}

func _web(addr string, db *sql.DB, a *Agent, stop chan struct{}) error {
	c := make(chan error)
	f := func(db *sql.DB, a *Agent) error {
		m := martini.Classic()

		m.Map(db)
		m.Map(a)
		m.Use(staticbin.Static("assets", Asset))
		m.Use(render.Renderer())

		m.Get("/", func(res http.ResponseWriter, req *http.Request, l *log.Logger, r render.Render) {
			index, err := assets_index_html()
			if err != nil {
				l.Printf("error: %v", err)
				r.Error(http.StatusInternalServerError)
				return
			}
			http.ServeContent(res, req, req.URL.Path, time.Now(), bytes.NewReader(index))
		})

		m.Group("/api", func(_r martini.Router) {
			_r.Get("/logs", func(res http.ResponseWriter, req *http.Request, db *sql.DB, r render.Render, l *log.Logger) {
				stmt := `
				select irlog.id as id, format, freq, data, hostname, deviceid, created_at, data_name.name as name from irlog
				left join data_name on irlog.data_hash = data_name.data_hash
				order by irlog.id desc
				limit 20
				`
				rows, err := db.Query(stmt)
				if err != nil {
					l.Printf("error: %v", err)
					r.Error(http.StatusInternalServerError)
					return
				}

				rs := []*IRLog{}

				for rows.Next() {

					i := &IRLog{}
					if err := i.BindFromScanWithName(rows); err != nil {
						l.Printf("error: %v", err)
						r.Error(http.StatusInternalServerError)
						return
					}

					rs = append(rs, i)
				}

				r.JSON(http.StatusOK, rs)
			})

			_r.Get("/log/:id", WebGetLogHandler)

			_r.Post("/log/:id", func(res http.ResponseWriter, req *http.Request, db *sql.DB, r render.Render, p martini.Params, l *log.Logger) {
				id := p["id"]
				name := req.PostFormValue("name")

				if len(name) == 0 {
					r.Error(http.StatusBadRequest)
					return
				}

				tx, err := db.Begin()
				if err != nil {
					l.Printf("error: %v", err)
					r.Error(http.StatusInternalServerError)
					return
				}
				defer tx.Rollback()

				irlog, err := FetchIRLogByIdWithoutName(tx, id)
				switch {
				case err == sql.ErrNoRows:
					r.Error(http.StatusNotFound)
					return
				case err != nil:
					l.Printf("error: %v", err)
					r.Error(http.StatusInternalServerError)
					return
				}

				stmt := `update data_name set name = ? where data_hash = ?`

				ret, err := tx.Exec(stmt, name, irlog.DataHash)
				if err != nil {
					l.Printf("error: %v", err)
					r.Error(http.StatusInternalServerError)
					return
				}

				affected, err := ret.RowsAffected()
				if err != nil {
					l.Printf("error: %v", err)
					r.Error(http.StatusInternalServerError)
					return
				}

				if affected == 0 {
					stmt = `insert into data_name(data_hash,name) values(?, ?)`
					if _, err := tx.Exec(stmt, irlog.DataHash, name); err != nil {
						l.Printf("error: %v", err)
						r.Error(http.StatusInternalServerError)
						return
					}
				}

				tx.Commit()

				WebGetLogHandler(res, req, db, r, p, l)
			})

			_r.Post("/log/:id/message", func(res http.ResponseWriter, req *http.Request, db *sql.DB, r render.Render, p martini.Params, a *Agent, l *log.Logger) {
				id := p["id"]

				irlog, err := FetchIRLogByIdWithoutName(db, id)
				switch {
				case err == sql.ErrNoRows:
					r.Error(http.StatusNotFound)
					return
				case err != nil:
					l.Printf("error: %v", err)
					r.Error(http.StatusInternalServerError)
					return
				}

				msg, err := irlog.Message()
				if err != nil {
					l.Printf("error: %v", err)
					r.Error(http.StatusInternalServerError)
					return
				}

				if err := a.Post(irlog.Deviceid, msg); err != nil {
					l.Printf("error: %v", err)
					r.Error(http.StatusInternalServerError)
					return
				}

				r.Status(http.StatusOK)
			})
		})

		log.Println("start", addr)
		return http.ListenAndServe(addr, m)
	}

	go func() { c <- f(db, a) }()

	select {
	case <-stop:
		return nil
	case err := <-c:
		return err
	}
}

func WebGetLogHandler(res http.ResponseWriter, req *http.Request, db *sql.DB, r render.Render, p martini.Params, l *log.Logger) {
	id := p["id"]

	stmt := `
	select irlog.id as id, format, freq, data, hostname, deviceid, created_at, data_name.name as name from irlog
	left join data_name on irlog.data_hash = data_name.data_hash
	where irlog.id = ?
	limit 1
	`

	row := db.QueryRow(stmt, id)
	i := &IRLog{}
	if err := i.BindFromScanWithName(row); err != nil {
		if err == sql.ErrNoRows {
			r.Error(http.StatusNotFound)
			return
		}
		l.Printf("error: %v", err)
		r.Error(http.StatusInternalServerError)
		return
	}

	r.JSON(http.StatusOK, i)
}

func FetchIRLogByIdWithoutName(q QueryRower, id string) (*IRLog, error) {
	var _id int
	var format string
	var freq int
	var data string
	var dataHash string
	var hostname string
	var deviceid string
	var createdAt int64

	stmt := `select id, format, freq, data, data_hash, hostname, deviceid, created_at from irlog where id = ?`
	if err := q.QueryRow(stmt, id).Scan(&_id, &format, &freq, &data, &dataHash, &hostname, &deviceid, &createdAt); err != nil {
		return nil, err
	}

	return &IRLog{
		Id:        _id,
		Format:    format,
		Freq:      freq,
		Data:      data,
		DataHash:  dataHash,
		Hostname:  hostname,
		Deviceid:  deviceid,
		CreatedAt: createdAt,
	}, nil
}
