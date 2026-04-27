package sugaar

import (
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

// Cookie returns a request cookie's value or "" if absent.
func (c *Context) Cookie(name string) string {
	ck, err := c.r.Cookie(name)
	if err != nil {
		return ""
	}
	return ck.Value
}

// SetCookie sets a response cookie. Pass http.Cookie directly when you need
// SameSite, Domain, etc.
func (c *Context) SetCookie(ck *http.Cookie) { http.SetCookie(c.w, ck) }

// ClientIP returns the request's best-guess client IP, honoring
// X-Forwarded-For (first hop) and X-Real-Ip when present, else RemoteAddr.
func (c *Context) ClientIP() string {
	if xff := c.r.Header.Get("X-Forwarded-For"); xff != "" {
		if comma := strings.IndexByte(xff, ','); comma > 0 {
			return strings.TrimSpace(xff[:comma])
		}
		return strings.TrimSpace(xff)
	}
	if rip := c.r.Header.Get("X-Real-Ip"); rip != "" {
		return rip
	}
	host := c.r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	return host
}

// Redirect sends an HTTP redirect. Use status 301/302/303/307/308.
func (c *Context) Redirect(status int, location string) error {
	http.Redirect(c.w, c.r, location, status)
	return nil
}

// NoContent writes status with an empty body.
func (c *Context) NoContent(status int) error {
	c.w.WriteHeader(status)
	return nil
}

// File serves a single file. The Content-Type is detected from the path.
func (c *Context) File(path string) error {
	http.ServeFile(c.w, c.r, path)
	return nil
}

// HTML renders a parsed template with data.
func (c *Context) HTML(status int, tmpl *template.Template, name string, data any) error {
	c.w.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.w.WriteHeader(status)
	if name == "" {
		return tmpl.Execute(c.w, data)
	}
	return tmpl.ExecuteTemplate(c.w, name, data)
}

// Stream copies r to the response body, flushing as data arrives if the
// underlying writer supports it. Useful for proxying long-running responses.
func (c *Context) Stream(status int, contentType string, r io.Reader) error {
	c.w.Header().Set("Content-Type", contentType)
	c.w.WriteHeader(status)
	flusher, _ := c.w.(http.Flusher)
	buf := make([]byte, 4<<10)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if _, werr := c.w.Write(buf[:n]); werr != nil {
				return werr
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// BindQuery decodes URL query parameters into a struct. Field names match
// `query:"..."` tags, falling back to the lowercased field name. Supported
// kinds: string, bool, int*, uint*, float*. Slices are not supported (yet).
func (c *Context) BindQuery(dst any) error {
	return decodeValues(c.r.URL.Query(), dst, "query")
}

// BindForm parses application/x-www-form-urlencoded (or multipart) and
// decodes into dst with `form:"..."` tags.
func (c *Context) BindForm(dst any) error {
	if err := c.r.ParseForm(); err != nil {
		return err
	}
	return decodeValues(c.r.Form, dst, "form")
}

func decodeValues(values url.Values, dst any, tag string) error {
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return errors.New("dst must be a non-nil pointer to a struct")
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return errors.New("dst must point to a struct")
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		key := f.Tag.Get(tag)
		if key == "" {
			key = strings.ToLower(f.Name)
		} else if key == "-" {
			continue
		}
		raw := values.Get(key)
		if raw == "" {
			continue
		}
		if err := setField(v.Field(i), raw); err != nil {
			return errors.New(key + ": " + err.Error())
		}
	}
	return nil
}

func setField(fv reflect.Value, raw string) error {
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		fv.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, fv.Type().Bits())
		if err != nil {
			return err
		}
		fv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, fv.Type().Bits())
		if err != nil {
			return err
		}
		fv.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(raw, fv.Type().Bits())
		if err != nil {
			return err
		}
		fv.SetFloat(f)
	default:
		return errors.New("unsupported field kind " + fv.Kind().String())
	}
	return nil
}
