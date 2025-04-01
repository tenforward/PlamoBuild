// Package log provides logging for rclone
package log

import (
	"context"
	"io"
	"log"
	"os"
	"reflect"
	"runtime"
	"strings"

	"github.com/rclone/rclone/fs"
	"github.com/sirupsen/logrus"
)

// OptionsInfo descripts the Options in use
var OptionsInfo = fs.Options{{
	Name:    "log_file",
	Default: "",
	Help:    "Log everything to this file",
	Groups:  "Logging",
}, {
	Name:    "log_format",
	Default: "date,time",
	Help:    "Comma separated list of log format options",
	Groups:  "Logging",
}, {
	Name:    "syslog",
	Default: false,
	Help:    "Use Syslog for logging",
	Groups:  "Logging",
}, {
	Name:    "syslog_facility",
	Default: "DAEMON",
	Help:    "Facility for syslog, e.g. KERN,USER",
	Groups:  "Logging",
}, {
	Name:    "log_systemd",
	Default: false,
	Help:    "Activate systemd integration for the logger",
	Groups:  "Logging",
}}

// Options contains options for controlling the logging
type Options struct {
	File              string `config:"log_file"`        // Log everything to this file
	Format            string `config:"log_format"`      // Comma separated list of log format options
	UseSyslog         bool   `config:"syslog"`          // Use Syslog for logging
	SyslogFacility    string `config:"syslog_facility"` // Facility for syslog, e.g. KERN,USER,...
	LogSystemdSupport bool   `config:"log_systemd"`     // set if using systemd logging
}

func init() {
	fs.RegisterGlobalOptions(fs.OptionsInfo{Name: "log", Opt: &Opt, Options: OptionsInfo})
}

// Opt is the options for the logger
var Opt Options

// fnName returns the name of the calling +2 function
func fnName() string {
	pc, _, _, ok := runtime.Caller(2)
	name := "*Unknown*"
	if ok {
		name = runtime.FuncForPC(pc).Name()
		dot := strings.LastIndex(name, ".")
		if dot >= 0 {
			name = name[dot+1:]
		}
	}
	return name
}

// Trace debugs the entry and exit of the calling function
//
// It is designed to be used in a defer statement, so it returns a
// function that logs the exit parameters.
//
// Any pointers in the exit function will be dereferenced
func Trace(o interface{}, format string, a ...interface{}) func(string, ...interface{}) {
	if fs.GetConfig(context.Background()).LogLevel < fs.LogLevelDebug {
		return func(format string, a ...interface{}) {}
	}
	name := fnName()
	fs.LogPrintf(fs.LogLevelDebug, o, name+": "+format, a...)
	return func(format string, a ...interface{}) {
		for i := range a {
			// read the values of the pointed to items
			typ := reflect.TypeOf(a[i])
			if typ.Kind() == reflect.Ptr {
				value := reflect.ValueOf(a[i])
				if value.IsNil() {
					a[i] = nil
				} else {
					pointedToValue := reflect.Indirect(value)
					a[i] = pointedToValue.Interface()
				}
			}
		}
		fs.LogPrintf(fs.LogLevelDebug, o, ">"+name+": "+format, a...)
	}
}

// Stack logs a stack trace of callers with the o and info passed in
func Stack(o interface{}, info string) {
	if fs.GetConfig(context.Background()).LogLevel < fs.LogLevelDebug {
		return
	}
	arr := [16 * 1024]byte{}
	buf := arr[:]
	n := runtime.Stack(buf, false)
	buf = buf[:n]
	fs.LogPrintf(fs.LogLevelDebug, o, "%s\nStack trace:\n%s", info, buf)
}

// InitLogging start the logging as per the command line flags
func InitLogging() {
	flagsStr := "," + Opt.Format + ","
	var flags int
	if strings.Contains(flagsStr, ",date,") {
		flags |= log.Ldate
	}
	if strings.Contains(flagsStr, ",time,") {
		flags |= log.Ltime
	}
	if strings.Contains(flagsStr, ",microseconds,") {
		flags |= log.Lmicroseconds
	}
	if strings.Contains(flagsStr, ",UTC,") {
		flags |= log.LUTC
	}
	if strings.Contains(flagsStr, ",longfile,") {
		flags |= log.Llongfile
	}
	if strings.Contains(flagsStr, ",shortfile,") {
		flags |= log.Lshortfile
	}
	log.SetFlags(flags)

	fs.LogPrintPid = strings.Contains(flagsStr, ",pid,")

	// Log file output
	if Opt.File != "" {
		f, err := os.OpenFile(Opt.File, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0640)
		if err != nil {
			fs.Fatalf(nil, "Failed to open log file: %v", err)
		}
		_, err = f.Seek(0, io.SeekEnd)
		if err != nil {
			fs.Errorf(nil, "Failed to seek log file to end: %v", err)
		}
		log.SetOutput(f)
		logrus.SetOutput(f)
		redirectStderr(f)
	}

	// Syslog output
	if Opt.UseSyslog {
		if Opt.File != "" {
			fs.Fatalf(nil, "Can't use --syslog and --log-file together")
		}
		startSysLog()
	}

	// Activate systemd logger support if systemd invocation ID is
	// detected and output is going to stderr (not logging to a file or syslog)
	if !Redirected() {
		if isJournalStream() {
			Opt.LogSystemdSupport = true
		}
	}

	// Systemd logging output
	if Opt.LogSystemdSupport {
		startSystemdLog()
	}
}

// Redirected returns true if the log has been redirected from stdout
func Redirected() bool {
	return Opt.UseSyslog || Opt.File != ""
}
