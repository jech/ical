package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
	rrule "github.com/teambition/rrule-go"
)

type config struct {
	Endpoint  string   `json:"endpoint"`
	Username  string   `json:"username,omitempty"`
	Password  string   `json:"password,omitempty"`
	Calendars []string `json:"calendars,omitempty"`
}

func main() {
	configdir, err := os.UserConfigDir()
	if err != nil {
		log.Fatalf("UserConfigDir: %v", err)
	}
	configFile := filepath.Join(
		filepath.Join(configdir, "ical"),
		"ical.json",
	)
	flag.StringVar(&configFile, "config", configFile, "configuration `file`")
	var verbose bool
	flag.BoolVar(&verbose, "v", false, "verbose, display event descriptions")
	var listCalendars bool
	flag.BoolVar(&listCalendars, "list", false,
		"list user's calendars and exit")
	var durationStr string
	flag.StringVar(&durationStr, "duration", "week",
		"time interval of interest")
	flag.Parse()

	var duration time.Duration
	switch durationStr {
	case "day":
		duration = 24 * time.Hour
	case "week":
		duration = 7 * 24 * time.Hour
	case "month":
		duration = 31 * 24 * time.Hour
	case "year":
		duration = 356 * 24 * time.Hour
	default:
		i, err := strconv.Atoi(durationStr)
		if err != nil {
			log.Fatalf("Couldn't parse interval %v: %v",
				durationStr, err,
			)
		}
		duration = time.Duration(i) * 24 * time.Hour
	}

	config, err := readConfig(configFile)
	if err != nil {
		log.Fatalf("read(%v): %v", configFile, err)
	}
	if config.Endpoint == "" {
		log.Fatalf("%v: no endpoint specified", configFile)
	}

	var hclient webdav.HTTPClient

	if config.Username != "" {
		hclient = webdav.HTTPClientWithBasicAuth(
			hclient, config.Username, config.Password,
		)
	} else {
		hclient = http.DefaultClient
	}
	client, err := caldav.NewClient(hclient, config.Endpoint)
	if err != nil {
		log.Fatalf("NewClient: %v", err)
	}

	var calendars []caldav.Calendar
	if !listCalendars && len(config.Calendars) > 0 {
		for _, pth := range config.Calendars {
			calendars = append(calendars, caldav.Calendar{
				Path: pth,
			})
		}
	} else {
		calendars, err = findCalendars(client)
		if err != nil {
			log.Fatalf("findCalendars: %v", err)
		}
	}

	if listCalendars {
		u, err := url.Parse(config.Endpoint)
		if err != nil {
			log.Fatalf("Cannot parse %v: %v", config.Endpoint, err)
		}
		root := u.Path
		for _, c := range calendars {
			pth, err := filepath.Rel(root, c.Path)
			if err != nil {
				pth = c.Path
			}
			fmt.Printf("%-24v %v\n", pth, c.Name)
			if verbose && c.Description != "" {
				fmt.Println(c.Description)
			}
		}
		return
	}

	start := time.Now()
	end := time.Now().Add(duration)
	es, err := queryEvents(client, calendars, start, end, verbose)
	if err != nil {
		log.Fatalf("queryEvents: %v", err)
	}

	for _, e := range es {
		printEvent(os.Stdout, e, verbose)
	}
}

func readConfig(filename string) (*config, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	d := json.NewDecoder(f)
	var c config
	err = d.Decode(&c)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func findCalendars(client *caldav.Client) ([]caldav.Calendar, error) {
	principal, err := client.FindCurrentUserPrincipal(context.Background())
	if err != nil {
		return nil, err
	}

	homeSet, err := client.FindCalendarHomeSet(
		context.Background(), principal,
	)
	if err != nil {
		return nil, err
	}

	return client.FindCalendars(context.Background(), homeSet)
}

type event struct {
	start, end                     time.Time
	summary, description, location string
}

func queryEvents(client *caldav.Client, calendars []caldav.Calendar, start, end time.Time, includeDescription bool) ([]event, error) {
	props := []string{"SUMMARY", "DTSTART", "DTEND", "LOCATION"}
	if includeDescription {
		props = append(props, "DESCRIPTION")
	}
	query := caldav.CalendarQuery{
		CompRequest: caldav.CalendarCompRequest{
			Name: "VCALENDAR",
			Comps: []caldav.CalendarCompRequest{{
				Name:  "VEVENT",
				Props: props,
			}},
		},
		CompFilter: caldav.CompFilter{
			Name: "VCALENDAR",
			Comps: []caldav.CompFilter{{
				Name:  "VEVENT",
				Start: start,
				End:   end,
			}},
		},
	}

	es := make([]event, 0)

	for _, c := range calendars {
		objs, err := client.QueryCalendar(
			context.Background(), c.Path, &query,
		)
		if err != nil {
			log.Printf("QueryCalendar(%v): %v", c.Name, err)
			continue
		}
		for _, o := range objs {
			for _, e := range o.Data.Events() {
				e, err := parseEvent(
					e, start, end, includeDescription,
				)
				if err != nil {
					log.Println("parseEvent:", err)
					continue
				}
				es = append(es, e...)
			}
		}
	}

	slices.SortFunc(es, func(a, b event) int {
		return a.start.Compare(b.start)
	})
	return es, nil
}

func parseEvent(e ical.Event, start, end time.Time, includeDescription bool) ([]event, error) {
	dtstart, _ := e.DateTimeStart(time.Local)
	dtend, _ := e.DateTimeEnd(time.Local)
	duration := dtend.Sub(dtstart)
	ropt, _ := e.Props.RecurrenceRule()
	summary, _ := e.Props.Text(ical.PropSummary)
	var description string
	if includeDescription {
		description, _ = e.Props.Text(
			ical.PropDescription,
		)
	}
	location, _ := e.Props.Text(
		ical.PropLocation,
	)
	if ropt != nil {
		ropt.Dtstart = dtstart
		rr, err := rrule.NewRRule(*ropt)
		if err != nil {
			return nil, err
		}
		ts := rr.Between(start, end, true)
		es := make([]event, 0, len(ts))
		for _, t := range ts {
			tend := t.Add(duration)
			ee := event{
				start:       t,
				end:         tend,
				summary:     summary,
				description: description,
				location:    location,
			}
			es = append(es, ee)
		}
		return es, nil
	}
	ee := event{
		start:       dtstart,
		end:         dtend,
		summary:     summary,
		description: description,
		location:    location,
	}
	return []event{ee}, nil
}

func printEvent(w io.Writer, e event, verbose bool) error {
	duration := e.end.Sub(e.start)
	var location string
	if e.location != "" {
		location = ", " + e.location
	}
	if e.start.Hour() == 0 && e.start.Minute() == 0 &&
		duration == 24*time.Hour {
		_, err := fmt.Fprintf(w,
			"%v              %v%v\n",
			e.start.Format("Mon 2006-01-02"),
			e.summary,
			location,
		)
		if err != nil {
			return err
		}
	} else {
		var d string
		if duration > 0 && duration < 24*time.Hour {
			h := int(duration / time.Hour)
			m := int((duration -
				time.Duration(h)*time.Hour) /
				time.Minute)
			if m == 0 {
				d = fmt.Sprintf("%2vh   ", h)
			} else {
				d = fmt.Sprintf("%2vh%02vm", h, m)
			}
		} else {
			d = duration.String()
		}
		_, err := fmt.Fprintf(w, "%v %v %v%v\n",
			e.start.Format("Mon 2006-01-02 15:04"),
			d,
			e.summary,
			location,
		)
		if err != nil {
			return err
		}
	}
	if verbose && e.description != "" {
		_, err := fmt.Fprintf(w, "%v\n", e.description)
		if err != nil {
			return err
		}
	}
	return nil
}
