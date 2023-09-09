# Consult CalDav (iCalendar) calendars from the command line

A command-line program to consult remote CalDav (iCalendar) calendars from
the command line.  There is no caching and no need to synchronise
anything: `ical` sends requests directly to the remote server.

## Building

Say

```
go build -ldflags="-s -w"
```
Then copy the `ical` binary somewhere on your path.

## The configuration file

When you first start `ical`, it will complain about a missing configuration
file (`~/.config/ical/ical.json` under Unix).  The file should look like this:
```
{
    "endpoint": "https://cloud.example.org/remote.php/dav/calendars/username",
    "username": "username",
    "password": "password"
}
```

Omit `"username"` and `"password"` if no authentication is needed.  If you
only want to consult a subset of calendars, add a field "calendars":
```
{
    "endpoint": "https://cloud.example.org/remote.php/dav/calendars/username",
    "username": "username",
    "password": "password",
    "calendars": ["personal"]
}
```

## Usage

Display all events in the next 7 days:
```
ical
```

Display all events in the next 31 days:
```
ical -duration month
```

Display all events in the next 3 days:
```
ical -duration 3
```

Display all events in the next 7 days together with their descriptions:
```
ical -v
```

List the user's calendars:
```
ical -list
```
