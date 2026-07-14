package licensestudio

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/licensing"
)

//go:embed assets/*
var studioAssets embed.FS

type ServerOptions struct {
	Dialogs      FileDialogs
	SessionToken string
	AllowedHost  string
	Now          func() time.Time
	OnActivity   func()
	Shutdown     func()
}

type server struct {
	dialogs      FileDialogs
	sessionToken string
	basePath     string
	allowedHost  string
	origin       string
	now          func() time.Time
	onActivity   func()
	shutdown     func()
}

type issueRequest struct {
	KeyID   string            `json:"key_id"`
	Payload licensing.Payload `json:"payload"`
}

type keygenRequest struct {
	Purpose string `json:"purpose"`
	KeyID   string `json:"key_id"`
}

type verifyRequest struct {
	DeploymentID string `json:"deployment_id"`
}

type errorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func NewServer(options ServerOptions) (http.Handler, error) {
	if options.Dialogs == nil {
		return nil, fmt.Errorf("file dialogs are required")
	}
	token := strings.TrimSpace(options.SessionToken)
	if token == "" || strings.ContainsAny(token, "/\\") {
		return nil, fmt.Errorf("a URL-safe session token is required")
	}
	host := strings.TrimSpace(options.AllowedHost)
	if host == "" {
		return nil, fmt.Errorf("allowed host is required")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &server{
		dialogs:      options.Dialogs,
		sessionToken: token,
		basePath:     "/" + token + "/",
		allowedHost:  host,
		origin:       "http://" + host,
		now:          now,
		onActivity:   options.OnActivity,
		shutdown:     options.Shutdown,
	}, nil
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.setSecurityHeaders(w)
	if !s.isLocalRequest(r) || r.Host != s.allowedHost || !strings.HasPrefix(r.URL.Path, s.basePath) {
		http.NotFound(w, r)
		return
	}
	if s.onActivity != nil {
		s.onActivity()
	}
	relative := strings.TrimPrefix(r.URL.Path, s.basePath)
	switch {
	case r.Method == http.MethodGet && (relative == "" || relative == "index.html"):
		s.serveAsset(w, "assets/index.html", "text/html; charset=utf-8")
	case r.Method == http.MethodGet && strings.HasPrefix(relative, "assets/"):
		s.serveAsset(w, relative, "")
	case r.Method == http.MethodPost && relative == "api/keygen":
		s.handleKeygen(w, r)
	case r.Method == http.MethodPost && relative == "api/issue":
		s.handleIssue(w, r)
	case r.Method == http.MethodPost && relative == "api/verify":
		s.handleVerify(w, r)
	case r.Method == http.MethodPost && relative == "api/shutdown":
		s.handleShutdown(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *server) serveAsset(w http.ResponseWriter, name, contentType string) {
	raw, err := studioAssets.ReadFile(name)
	if err != nil {
		http.NotFound(w, nil)
		return
	}
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(name))
	}
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (s *server) handleKeygen(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAPI(w, r) {
		return
	}
	var request keygenRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	directory, err := s.dialogs.ChooseDirectory(r.Context(), "选择保存 UniRouter 签名密钥的安全文件夹")
	if err != nil {
		s.writeOperationError(w, err)
		return
	}
	result, err := GenerateKeyFiles(directory, request.Purpose, request.KeyID)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "key_generation_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *server) handleIssue(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAPI(w, r) {
		return
	}
	var request issueRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	privatePath, err := s.dialogs.ChooseOpenFile(r.Context(), "选择对应 key_id 的 License 私钥")
	if err != nil {
		s.writeOperationError(w, err)
		return
	}
	defaultName := safeFileName(request.Payload.LicenseID) + ".license.json"
	outputPath, err := s.dialogs.ChooseSaveFile(r.Context(), "保存已签名的客户 License", defaultName)
	if err != nil {
		s.writeOperationError(w, err)
		return
	}
	result, err := IssueLicense(privatePath, outputPath, request.KeyID, request.Payload)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "license_issue_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *server) handleVerify(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAPI(w, r) {
		return
	}
	var request verifyRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	publicPath, err := s.dialogs.ChooseOpenFile(r.Context(), "选择 License 公钥环 JSON")
	if err != nil {
		s.writeOperationError(w, err)
		return
	}
	licensePath, err := s.dialogs.ChooseOpenFile(r.Context(), "选择需要验证的 License JSON")
	if err != nil {
		s.writeOperationError(w, err)
		return
	}
	status := VerifyLicense(publicPath, licensePath, request.DeploymentID, s.now().UTC())
	writeJSON(w, http.StatusOK, status)
}

func (s *server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAPI(w, r) {
		return
	}
	var request map[string]interface{}
	if !decodeRequest(w, r, &request) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"shutting_down": true})
	if s.shutdown != nil {
		go func() {
			time.Sleep(100 * time.Millisecond)
			s.shutdown()
		}()
	}
}

func (s *server) authorizeAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Origin") != s.origin || r.Header.Get("X-UniRouter-Studio-Token") != s.sessionToken {
		writeError(w, http.StatusForbidden, "forbidden", "invalid local session origin or token")
		return false
	}
	if mediaType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])); mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "invalid_content_type", "application/json is required")
		return false
	}
	return true
}

func (s *server) isLocalRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func (s *server) setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
}

func (s *server) writeOperationError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrDialogCancelled) {
		writeError(w, http.StatusConflict, "cancelled", "已取消文件选择，未写入任何新文件")
		return
	}
	writeError(w, http.StatusInternalServerError, "file_dialog_failed", err.Error())
}

func decodeRequest(w http.ResponseWriter, r *http.Request, destination interface{}) bool {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64*1024+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("请求 JSON 无效：%v", err))
		return false
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("unexpected trailing JSON value")
		}
		writeError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("请求 JSON 无效：%v", err))
		return false
	}
	return true
}

func safeFileName(value string) string {
	value = strings.TrimSpace(value)
	var builder strings.Builder
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' {
			builder.WriteRune(char)
		} else {
			builder.WriteByte('-')
		}
	}
	name := strings.Trim(builder.String(), ".-")
	if name == "" {
		return "unirouter"
	}
	if len(name) > 120 {
		name = name[:120]
	}
	return name
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	response := errorResponse{}
	response.Error.Code = code
	response.Error.Message = message
	writeJSON(w, status, response)
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
