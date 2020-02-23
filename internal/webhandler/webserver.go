package webhandler

import (
	"crypto/tls"
	"encoding/base64"
	"html/template"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gravitational/force/api"
	"github.com/gravitational/force/internal/cert"

	"github.com/gravitational/httplib"
	"github.com/gravitational/httplib/csrf"
	"github.com/gravitational/trace"
	"github.com/julienschmidt/httprouter"
	log "github.com/sirupsen/logrus"
)

// New returns unstarted web handler
func New() (http.Handler, error) {
	// web handler serves classic HTTP API used by OIDC
	// and login forms and static assets
	webHandler, err := NewWebHandler()
	if err != nil {
		return nil, trace.Wrap(err)
	}
	// GPRC Handler serves GRPC api over HTTP 2.0 and websocket
	return api.NewGRPCHandler(webHandler), nil
}

// Handler is a web handler
type Handler struct {
	httprouter.Router
}

func (h *Handler) ping(w http.ResponseWriter, r *http.Request, p httprouter.Params) (interface{}, error) {
	return "ok", nil
}

// NewWebHandler returns a web handler
func NewWebHandler() (http.Handler, error) {
	h := &Handler{}

	// ping endpoint is used to check if the server is up. the /webapi/ping
	// endpoint returns the default authentication method and configuration that
	// the server supports. the /webapi/ping/:connector endpoint can be used to
	// query the authentication configuration for a specific connector.
	h.GET("/api/ping", httplib.MakeHandler(h.ping))

	staticFS, err := NewStaticFileSystem()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	index, err := staticFS.Open("/index.html")
	if err != nil {
		log.Error(err)
		return nil, trace.Wrap(err)
	}
	defer index.Close()
	indexContent, err := ioutil.ReadAll(index)
	if err != nil {
		return nil, trace.ConvertSystemError(err)
	}

	indexPage, err := template.New("index").Parse(string(indexContent))
	if err != nil {
		return nil, trace.BadParameter("failed parsing index.html template: %v", err)
	}

	routingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// redirect to "/web" when someone hits "/"
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/web", http.StatusFound)
			return
		}

		// serve Web UI, all web properties
		// exist under /web
		//
		// static assets are served from /web/dist path
		if strings.HasPrefix(r.URL.Path, "/web/dist") {
			httplib.SetStaticFileHeaders(w.Header())
			http.StripPrefix("/web/dist", http.FileServer(staticFS)).ServeHTTP(w, r)
		} else if strings.HasPrefix(r.URL.Path, "/web/") || r.URL.Path == "/web" {
			// dynamic pages are served via /web/ root path
			csrfToken, err := csrf.AddCSRFProtection(w, r)
			if err != nil {
				log.Errorf("failed to generate CSRF token %v", err)
			}
			session := struct {
				Session string
				XCSRF   string
			}{
				XCSRF:   csrfToken,
				Session: base64.StdEncoding.EncodeToString([]byte("{}")),
			}

			httplib.SetIndexHTMLHeaders(w.Header())
			indexPage.Execute(w, session)
		} else {
			http.NotFound(w, r)
		}
	})

	h.NotFound = routingHandler
	return h, nil
}

// NewStaticFileSystem returns the initialized implementation of http.FileSystem
// interface which can be used to serve Teleport Proxy Web UI
//
// If 'debugMode' is true, it will load the web assets from the same git repo
// directory where the executable is, otherwise it will load them from the embedded
// zip archive.
//
func NewStaticFileSystem() (http.FileSystem, error) {
	assetsToCheck := []string{"index.html"}

	assetsPath := "./web/dist"
	for _, af := range assetsToCheck {
		_, err := os.Stat(filepath.Join(assetsPath, af))
		if err != nil {
			return nil, trace.Wrap(err)
		}
	}
	log.Infof("[Web] Using filesystem for serving web assets: %s", assetsPath)
	return http.Dir(assetsPath), nil
}

// InitSelfSignedHTTPSCert generates and self-signs a TLS key+cert pair for https connection
// to the proxy server.
func InitSelfSignedHTTPSCert(certPath, keyPath string) (err error) {
	log.Warningf("No TLS Keys provided, using self signed certificate.")

	// return the existing pair if they have already been generated:
	_, err = tls.LoadX509KeyPair(certPath, keyPath)
	if err == nil {
		return nil
	}
	if !os.IsNotExist(err) {
		return trace.Wrap(err, "unrecognized error reading certs")
	}
	log.Warningf("Generating self signed key and cert to %v %v.", keyPath, certPath)

	creds, err := cert.GenerateSelfSignedCert([]string{"localhost"})
	if err != nil {
		return trace.Wrap(err)
	}

	if err := ioutil.WriteFile(keyPath, creds.PrivateKey, 0600); err != nil {
		return trace.Wrap(err, "error writing key PEM")
	}
	if err := ioutil.WriteFile(certPath, creds.Cert, 0600); err != nil {
		return trace.Wrap(err, "error writing key PEM")
	}
	return nil
}
