package getty

import (
	"bytes"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"
)

import (
	jerrors "github.com/juju/errors"
	"github.com/stretchr/testify/assert"
)

type PackageHandler struct{}

func (h *PackageHandler) Read(ss Session, data []byte) (interface{}, int, error) {
	return nil, 0, nil
}

func (h *PackageHandler) Write(ss Session, pkg interface{}) ([]byte, error) {
	return nil, nil
}

type MessageHandler struct {
	lock  sync.Mutex
	array []Session
}

func newMessageHandler() *MessageHandler {
	return &MessageHandler{}
}

func (h *MessageHandler) SessionNumber() int {
	h.lock.Lock()
	connNum := len(h.array)
	h.lock.Unlock()

	return connNum
}

func (h *MessageHandler) OnOpen(session Session) error {
	h.lock.Lock()
	defer h.lock.Unlock()
	h.array = append(h.array, session)

	return nil
}
func (h *MessageHandler) OnError(session Session, err error)         {}
func (h *MessageHandler) OnClose(session Session)                    {}
func (h *MessageHandler) OnMessage(session Session, pkg interface{}) {}
func (h *MessageHandler) OnCron(session Session)                     {}

type Package struct{}

func (p Package) String() string {
	return ""
}
func (p Package) Marshal() (*bytes.Buffer, error)           { return nil, nil }
func (p *Package) Unmarshal(buf *bytes.Buffer) (int, error) { return 0, nil }

var (
	pkg        Package
	pkgHandler PackageHandler
)

func newSessionCallback(session Session, handler *MessageHandler) error {
	session.SetName("hello-client-session")
	session.SetMaxMsgLen(1024)
	session.SetPkgHandler(&pkgHandler)
	session.SetEventListener(handler)
	session.SetRQLen(4)
	session.SetWQLen(32)
	session.SetReadTimeout(3e9)
	session.SetWriteTimeout(3e9)
	session.SetCronPeriod((int)(30e9 / 1e6))
	session.SetWaitTime(3e9)
	session.SetTaskPool(nil)

	return nil
}

func TestTCPClient(t *testing.T) {
	listenLocalServer := func() (net.Listener, error) {
		listener, err := net.Listen("tcp", ":0")
		if err != nil {
			return nil, err
		}

		go http.Serve(listener, nil)
		return listener, nil
	}

	listener, err := listenLocalServer()
	assert.Nil(t, err)
	assert.NotNil(t, listener)

	addr := listener.Addr().(*net.TCPAddr)
	t.Logf("server addr: %v", addr)
	clt := newClient(TCP_CLIENT,
		WithServerAddress(addr.String()),
		WithReconnectInterval(5e8),
		WithConnectionNumber(1),
	)
	assert.NotNil(t, clt)
	assert.True(t, clt.ID() > 0)
	assert.Equal(t, clt.endPointType, TCP_CLIENT)

	var (
		msgHandler MessageHandler
	)
	cb := func(session Session) error {
		return newSessionCallback(session, &msgHandler)
	}

	clt.RunEventLoop(cb)
	time.Sleep(1e9)

	assert.Equal(t, 1, msgHandler.SessionNumber())
	clt.Close()
	assert.True(t, clt.IsClosed())
}

func TestUDPClient(t *testing.T) {
	var (
		err  error
		conn *net.UDPConn
	)
	func() {
		srcAddr := &net.UDPAddr{IP: net.IPv4zero, Port: 0}
		conn, err = net.ListenUDP("udp", srcAddr)
		assert.Nil(t, err)
		assert.NotNil(t, conn)
	}()
	defer conn.Close()

	addr := conn.LocalAddr()
	t.Logf("server addr: %v", addr)
	clt := NewUDPClient(
		WithServerAddress(addr.String()),
		WithReconnectInterval(5e8),
		WithConnectionNumber(1),
	)
	assert.NotNil(t, clt)
	assert.True(t, clt.ID() > 0)
	//assert.Equal(t, clt.endPointType, UDP_CLIENT)

	var (
		msgHandler MessageHandler
	)
	cb := func(session Session) error {
		return newSessionCallback(session, &msgHandler)
	}

	clt.RunEventLoop(cb)
	time.Sleep(1e9)

	assert.Equal(t, 1, msgHandler.SessionNumber())
	ss := msgHandler.array[0]
	err = ss.WritePkg(nil, 0)
	assert.NotNil(t, err)
	err = ss.WritePkg([]byte("hello"), 0)
	assert.NotNil(t, jerrors.Cause(err))
	//host, port, _ := net.SplitHostPort(addr.String())
	//if len(host) < 8 {
	//	host = "127.0.0.1"
	//}
	//remotePort, _ := strconv.Atoi(port)
	//serverAddr := net.UDPAddr{IP: net.ParseIP(host), Port: remotePort}
	//udpCtx := UDPContext{
	//	Pkg:      []byte("hello"),
	//	PeerAddr: &serverAddr,
	//}
	//ss.WritePkg(udpCtx, 1e9)

	clt.Close()
	assert.True(t, clt.IsClosed())
	msgHandler.array[0].Reset()
	assert.Nil(t, msgHandler.array[0].Conn())
	//ss.WritePkg([]byte("hello"), 0)
}

func TestNewWSClient(t *testing.T) {
	var (
		server           Server
		serverMsgHandler MessageHandler
	)
	addr := "127.0.0.1:65000"
	path := "/hello"
	func() {
		server = NewWSServer(
			WithLocalAddress(addr),
			WithWebsocketServerPath(path),
		)
		newServerSession := func(session Session) error {
			return newSessionCallback(session, &serverMsgHandler)
		}
		go server.RunEventLoop(newServerSession)
	}()
	time.Sleep(1e9)

	client := NewWSClient(
		WithServerAddress("ws://"+addr+path),
		WithConnectionNumber(1),
	)

	var (
		msgHandler MessageHandler
	)
	cb := func(session Session) error {
		return newSessionCallback(session, &msgHandler)
	}

	client.RunEventLoop(cb)
	time.Sleep(1e9)

	assert.Equal(t, 1, msgHandler.SessionNumber())
	client.Close()
	assert.True(t, client.IsClosed())
	server.Close()
	assert.True(t, server.IsClosed())
}

var (
	WssServerCRT = []byte(`-----BEGIN CERTIFICATE-----
MIICHjCCAYegAwIBAgIQKpKqamBqmZ0hfp8sYb4uNDANBgkqhkiG9w0BAQsFADAS
MRAwDgYDVQQKEwdBY21lIENvMCAXDTcwMDEwMTAwMDAwMFoYDzIwODQwMTI5MTYw
MDAwWjASMRAwDgYDVQQKEwdBY21lIENvMIGfMA0GCSqGSIb3DQEBAQUAA4GNADCB
iQKBgQC5Nxsk6WjeaYazRYiGxHZ5G3FXSlSjV7lZeebItdEPzO8kVPIGCSTy/M5X
Nnpp3uVDFXQub0/O5t9Y6wcuqpUGMOV+XL7MZqSZlodXm0XhNYzCAjZ+URNjTHGP
NXIqdDEG5Ba8SXMOfY6H97+QxugZoAMFZ+N83ggr12IYNO/FbQIDAQABo3MwcTAO
BgNVHQ8BAf8EBAMCAqQwEwYDVR0lBAwwCgYIKwYBBQUHAwEwDwYDVR0TAQH/BAUw
AwEB/zA5BgNVHREEMjAwgglsb2NhbGhvc3SCC2V4YW1wbGUuY29thwR/AAABhxAA
AAAAAAAAAAAAAAAAAAABMA0GCSqGSIb3DQEBCwUAA4GBAE5dr9q7ORmKZ7yZqeSL
305armc13A7UxffUajeJFujpl2jOqnb5PuKJ7fn5HQKGB0qSq3IHsFua2WONXcTW
Vn4gS0k50IaDpW+yl+ArIo0QwbjPIAcFysX10p9dVO7A1uEpHbRDzefem6r9uVGk
i7dOLEoC8hkfk6nJsNEIEqu6
-----END CERTIFICATE-----`)
	WssServerCRTFile = "/tmp/server.crt"
	WssServerKEY     = []byte(`-----BEGIN RSA PRIVATE KEY-----
MIICXgIBAAKBgQC5Nxsk6WjeaYazRYiGxHZ5G3FXSlSjV7lZeebItdEPzO8kVPIG
CSTy/M5XNnpp3uVDFXQub0/O5t9Y6wcuqpUGMOV+XL7MZqSZlodXm0XhNYzCAjZ+
URNjTHGPNXIqdDEG5Ba8SXMOfY6H97+QxugZoAMFZ+N83ggr12IYNO/FbQIDAQAB
AoGBAJgvuXQY/fxSxUWkysvBvn9Al17cSrN0r23gBkvBaakMASvfSIbBGMU4COwM
bYV0ivkWNcK539/oQHk1lU85Bv0K9V9wtuFrYW0mN3TU6jnl6eEnzW5oy0Z9TwyY
wuGQOSXGr/aDVu8Wr7eOmSvn6j8rWO2dSMHCllJnSBoqQ1aZAkEA5YQspoMhUaq+
kC53GTgMhotnmK3fWfWKrlLf0spsaNl99W3+plwqxnJbye+5uEutRR1PWSWCCKq5
bN9veOXViwJBAM6WS5aeKO/JX09O0Ang9Y0+atMKO0YjX6fNFE2UJ5Ewzyr4DMZK
TmBpyzm4x/GhV9ukqcDcd3dNlUOtgRqY3+cCQQDCGmssk1+dUpqBE1rT8CvfqYv+
eqWWzerwDNSPz3OppK4630Bqby4Z0GNCP8RAUXgDKIuPqAH11HSm17vNcgqLAkA8
8FCzyUvCD+CxgEoV3+oPFA5m2mnJsr2QvgnzKHTTe1ZhEnKSO3ELN6nfCQbR3AoS
nGwGnAIRiy0wnYmr0tSZAkEAsWFm/D7sTQhX4Qnh15ZDdUn1WSWjBZevUtJnQcpx
TjihZq2sd3uK/XrzG+w7B+cPZlrZtQ94sDSVQwWl/sxB4A==
-----END RSA PRIVATE KEY-----`)
	WssServerKEYFile = "/tmp/server.key"
	WssClientCRT     = []byte(`-----BEGIN CERTIFICATE-----
MIICHjCCAYegAwIBAgIQKpKqamBqmZ0hfp8sYb4uNDANBgkqhkiG9w0BAQsFADAS
MRAwDgYDVQQKEwdBY21lIENvMCAXDTcwMDEwMTAwMDAwMFoYDzIwODQwMTI5MTYw
MDAwWjASMRAwDgYDVQQKEwdBY21lIENvMIGfMA0GCSqGSIb3DQEBAQUAA4GNADCB
iQKBgQC5Nxsk6WjeaYazRYiGxHZ5G3FXSlSjV7lZeebItdEPzO8kVPIGCSTy/M5X
Nnpp3uVDFXQub0/O5t9Y6wcuqpUGMOV+XL7MZqSZlodXm0XhNYzCAjZ+URNjTHGP
NXIqdDEG5Ba8SXMOfY6H97+QxugZoAMFZ+N83ggr12IYNO/FbQIDAQABo3MwcTAO
BgNVHQ8BAf8EBAMCAqQwEwYDVR0lBAwwCgYIKwYBBQUHAwEwDwYDVR0TAQH/BAUw
AwEB/zA5BgNVHREEMjAwgglsb2NhbGhvc3SCC2V4YW1wbGUuY29thwR/AAABhxAA
AAAAAAAAAAAAAAAAAAABMA0GCSqGSIb3DQEBCwUAA4GBAE5dr9q7ORmKZ7yZqeSL
305armc13A7UxffUajeJFujpl2jOqnb5PuKJ7fn5HQKGB0qSq3IHsFua2WONXcTW
Vn4gS0k50IaDpW+yl+ArIo0QwbjPIAcFysX10p9dVO7A1uEpHbRDzefem6r9uVGk
i7dOLEoC8hkfk6nJsNEIEqu6
-----END CERTIFICATE-----`)
	WssClientCRTFile = "/tmp/client.crt"
)

func DownloadFile(filepath string, content []byte) error {
	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Write the body to file
	_, err = out.Write(content)
	return err
}

func TestNewWSSClient(t *testing.T) {
	var (
		err              error
		server           Server
		serverMsgHandler MessageHandler
	)

	os.Remove(WssServerCRTFile)
	err = DownloadFile(WssServerCRTFile, WssServerCRT)
	assert.Nil(t, err)
	defer os.Remove(WssServerCRTFile)

	os.Remove(WssServerKEYFile)
	err = DownloadFile(WssServerKEYFile, WssServerKEY)
	assert.Nil(t, err)
	defer os.Remove(WssServerKEYFile)

	os.Remove(WssClientCRTFile)
	err = DownloadFile(WssClientCRTFile, WssClientCRT)
	assert.Nil(t, err)
	defer os.Remove(WssClientCRTFile)

	addr := "127.0.0.1:63450"
	path := "/hello"
	func() {
		server = NewWSSServer(
			WithLocalAddress(addr),
			WithWebsocketServerPath(path),
			WithWebsocketServerCert(WssServerCRTFile),
			WithWebsocketServerPrivateKey(WssServerKEYFile),
		)
		newServerSession := func(session Session) error {
			return newSessionCallback(session, &serverMsgHandler)
		}
		go server.RunEventLoop(newServerSession)
	}()
	time.Sleep(1e9)

	client := NewWSSClient(
		WithServerAddress("wss://"+addr+path),
		WithConnectionNumber(1),
		WithRootCertificateFile(WssClientCRTFile),
	)

	var (
		msgHandler MessageHandler
	)
	cb := func(session Session) error {
		return newSessionCallback(session, &msgHandler)
	}

	client.RunEventLoop(cb)
	time.Sleep(1e9)

	assert.Equal(t, 1, msgHandler.SessionNumber())
	client.Close()
	assert.True(t, client.IsClosed())
	assert.False(t, server.IsClosed())
	//time.Sleep(1000e9)
	//server.Close()
	//assert.True(t, server.IsClosed())
}
