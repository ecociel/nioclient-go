package nioclient

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
)

type Resource interface {
	Requires(principalOrToken string, method string) (ns Ns, obj Obj, rel Rel)
}

type responseWriterWrapper struct {
	http.ResponseWriter
	ip                    string
	time                  time.Time
	method, uri, protocol string
	status                int
	responseBytes         int64
	elapsedTime           time.Duration
	userAgent             string
	headersSent           bool
}

func Observe(w http.ResponseWriter, r *http.Request, f func(w http.ResponseWriter) error) {
	clientIP := r.RemoteAddr
	if colon := strings.LastIndex(clientIP, ":"); colon != -1 {
		clientIP = clientIP[:colon]
	}

	rw := &responseWriterWrapper{
		ResponseWriter: w,
		ip:             clientIP,
		time:           time.Time{},
		method:         r.Method,
		uri:            r.RequestURI,
		protocol:       r.Proto,
		status:         http.StatusOK,
		elapsedTime:    time.Duration(0),
		userAgent:      r.UserAgent(),
	}
	startTime := time.Now()
	err := f(rw)
	finishTime := time.Now()
	rw.time = finishTime.UTC()
	rw.elapsedTime = finishTime.Sub(startTime)

	if err != nil {
		errMsg := mapError(err, rw, r)
		if errMsg != "" {
			log.Printf("%s %s: error=%s identity=%s duration=%s", r.Method, r.RequestURI, errMsg, "-", rw.elapsedTime.String())
		}
	}
}

func mapError(err error, w *responseWriterWrapper, req *http.Request) (errMsg string) {

	var problem problemer
	if errors.As(err, &problem) {
		http.Error(w, fmt.Sprintf("%s: %s", problem.Error(), problem.Detail()), problem.Status())
		return ""
	}

	http.Error(w, "", http.StatusInternalServerError)
	errMsg = fmt.Sprintf("%v", err)
	return errMsg
}

// HandlerFunc is a specialized handler type that provides the following features:
//   - passes a Resource to the handler that can be used to access the extracted parameters
//   - passes a User to the handler that can be used to access the authenticated user
//     and perform further authorize checks
//   - allows the handler to return an error. This error can implement the problemer interface
//     to control how error response is constructured.
type HandlerFunc func(http.ResponseWriter, *http.Request, httprouter.Params, Resource, User) error

type Meter interface {
}
type Wrapper interface {
	Meter
	Check(ctx context.Context, ns Ns, obj Obj, rel Rel, userId UserId) (principal Principal, ok bool, err error)
	CheckWithTimestamp(ctx context.Context, ns Ns, obj Obj, rel Rel, userId UserId, ts Timestamp) (principal Principal, ok bool, err error)
	List(ctx context.Context, ns Ns, rel Rel, userId UserId) ([]string, error)
}

// TODO const None = Rel("none")
const Impossible = Rel("impossible")

func Wrap(wrapper Wrapper, extract func(r *http.Request, p httprouter.Params) (Resource, error), hdl HandlerFunc) httprouter.Handle {
	return httprouter.Handle(func(rw http.ResponseWriter, r *http.Request, p httprouter.Params) {

		sessionCookie, err := r.Cookie("session")
		if errors.Is(err, http.ErrNoCookie) {
			back := url.QueryEscape(r.RequestURI)
			uri := fmt.Sprintf("/signin?back=%s", back)
			http.Redirect(rw, r, uri, http.StatusSeeOther)
			return
		}

		checkFunc := wrapper.Check

		// If we have a check-timestamp hint, overwrite the checkfunc
		checkTimestampCookie, err := r.Cookie("check_ts")
		if err == nil {
			checkTimestamp := Timestamp(checkTimestampCookie.Value)
			if checkTimestampCookie.Value == "" {
				checkTimestamp = TimestampEpoch()
			}
			log.Printf("Check timestamp: %s", checkTimestamp)
			checkFunc = func(ctx context.Context, ns Ns, obj Obj, rel Rel, userId UserId) (principal Principal, ok bool, err error) {
				return wrapper.CheckWithTimestamp(ctx, ns, obj, rel, userId, checkTimestamp)
			}
		}

		token := sessionCookie.Value

		Observe(rw, r, func(w http.ResponseWriter) error {
			resource, err := extract(r, p)
			if err != nil {
				return fmt.Errorf("extract: %w", err)
			}
			ns, obj, rel := resource.Requires(token, r.Method)
			fmt.Printf("Access - %s,%s,%s\n", ns, obj, rel)

			principal, ok, err := checkFunc(r.Context(), ns, obj, rel, UserId(token))
			if err != nil {
				return fmt.Errorf("check: %w", err)
			}
			if !ok {
				w.WriteHeader(http.StatusForbidden)
				return nil
			}

			user := user{
				ns:        ns,
				obj:       obj,
				principal: principal,
				ctx:       r.Context(),
				check:     checkFunc,
				list:      wrapper.List,
			}

			return hdl(w, r, p, resource, &user)
		})
	})
}

//func validateCookieValueAndSetTimestamp(timestampCookieVal string, nowUtcMillis string) Timestamp {
//	parts := strings.SplitN(timestampCookieVal, ":", 2)
//	if len(parts) == 2 {
//		return Timestamp(parts[1])
//	} else {
//		return Timestamp(nowUtcMillis)
//	}
//}

type BasicWrapper interface {
	Authenticate(ctx context.Context, username, password []byte) (bool, error)
}

func BasicWrap(wrapper BasicWrapper, extract func(r *http.Request, p httprouter.Params) (Resource, error), hdl HandlerFunc) httprouter.Handle {
	return httprouter.Handle(func(rw http.ResponseWriter, r *http.Request, p httprouter.Params) {

		username, password, ok := r.BasicAuth()
		if !ok {
			rw.Header().Set("WWW-Authenticate", `Basic realm="TODO"`)
			rw.WriteHeader(http.StatusUnauthorized)
			return
		}

		Observe(rw, r, func(w http.ResponseWriter) error {
			ok, err := wrapper.Authenticate(r.Context(), []byte(username), []byte(password))
			if err != nil {
				return fmt.Errorf("authenticate basic: %w", err)
			}

			if !ok {
				rw.Header().Set("WWW-Authenticate", `Basic realm="TODO"`)
				rw.WriteHeader(http.StatusUnauthorized)
				return nil
			}

			resource, err := extract(r, p)
			if err != nil {
				return fmt.Errorf("extract: %w", err)
			}
			ns, obj, rel := resource.Requires(username, r.Method)
			_ = rel

			// TODO
			//principal, ok, err := checkFunc(r.Context(), ns, obj, rel, UserId(token))
			//if err != nil {
			//	return fmt.Errorf("check: %w", err)
			//}
			//if !ok {
			//	w.WriteHeader(http.StatusForbidden)
			//	return nil
			//}

			user := user{
				ns:        ns,
				obj:       obj,
				principal: Principal(username), // TODOprincipal,
				ctx:       r.Context(),
				check:     nil, //checkFunc,
				list:      nil, //wrapper.List,
			}

			return hdl(w, r, p, resource, &user)
		})
	})
}
