package handler

import (
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"reliquary/internal/chatgpt"
	"reliquary/internal/utils"
)

type ChatGPTHandler struct{ manager *chatgpt.Manager }

func NewChatGPTHandler(manager *chatgpt.Manager) *ChatGPTHandler {
	return &ChatGPTHandler{manager: manager}
}

func chatGPTCallbackURL() string {
	if value := strings.TrimSpace(os.Getenv("CHATGPT_CALLBACK_URL")); value != "" {
		return value
	}
	return "http://127.0.0.1:8080/auth/callback"
}

func chatGPTErrorCode(err error) string {
	var retry *chatgpt.AuthorizationRetryError
	if errors.As(err, &retry) {
		return "authorization_retry_required"
	}
	switch {
	case errors.Is(err, chatgpt.ErrEncryptionRequired):
		return "encryption_required"
	case errors.Is(err, chatgpt.ErrInvalidCallback):
		return "invalid_callback"
	case errors.Is(err, chatgpt.ErrInvalidState):
		return "invalid_state"
	case errors.Is(err, chatgpt.ErrInvalidIdentity):
		return "invalid_identity"
	case errors.Is(err, chatgpt.ErrInvalidClient):
		return "invalid_client"
	case errors.Is(err, chatgpt.ErrInsufficientScopes):
		return "insufficient_scopes"
	case errors.Is(err, chatgpt.ErrNeedsLogin):
		return "needs_login"
	case errors.Is(err, gorm.ErrRecordNotFound):
		return "account_not_found"
	default:
		return "chatgpt_unavailable"
	}
}

func chatGPTFailure(c *gin.Context, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, gorm.ErrRecordNotFound) {
		status = http.StatusNotFound
	}
	c.JSON(status, gin.H{"success": false, "error": gin.H{"code": chatGPTErrorCode(err), "message": chatGPTErrorCode(err)}})
}

func (h *ChatGPTHandler) StartLogin(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req struct {
		AccountID string `json:"account_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": gin.H{"code": "invalid_request", "message": "invalid_request"}})
		return
	}
	start, err := h.manager.BeginLogin(c.Request.Context(), chatGPTCallbackURL(), req.AccountID)
	if err != nil {
		chatGPTFailure(c, err)
		return
	}
	// Frontend receives only a short-lived local launch link, never an ID token.
	start.AuthorizationURL = chatGPTLaunchURL(chatGPTCallbackURL(), start.State)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": start})
}

func chatGPTLaunchURL(callbackURL, state string) string {
	launch, _ := url.Parse(callbackURL)
	launch.Path = "/auth/chatgpt/continue"
	launch.RawQuery = url.Values{"authorization_attempt": {state}}.Encode()
	return launch.String()
}

func (h *ChatGPTHandler) ContinueLogin(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	if len(c.Request.URL.Query()["authorization_attempt"]) != 1 {
		chatGPTFailure(c, chatgpt.ErrInvalidState)
		return
	}
	endpoint, err := h.manager.LoginAuthorizationURL(c.Request.Context(), c.Query("authorization_attempt"))
	if err != nil {
		chatGPTFailure(c, err)
		return
	}
	// A GET redirect helper renders the complete authorization URL in an HTML
	// body. Send only Location so retained ID-token hints never enter body logs.
	c.Header("Location", endpoint)
	c.Status(http.StatusSeeOther)
}

var chatGPTCallbackPage = template.Must(template.New("chatgpt-callback").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Reliquary ChatGPT</title></head>
<body><main><h1>{{.Heading}}</h1><p>{{.Message}}</p><p>{{.English}}</p>{{if .RetryURL}}<p><a href="{{.RetryURL}}">Continue with ChatGPT</a></p>{{end}}<p><a href="{{.SettingsURL}}">返回模型设置 / Return to model settings</a></p><p>可以关闭此标签页。 / You can close this tab.</p></main></body></html>`))

func chatGPTSettingsURL(result, accountID string) string {
	raw := strings.TrimSpace(os.Getenv("CHATGPT_SETTINGS_URL"))
	if raw == "" {
		raw = "http://127.0.0.1/platform/settings?section=models"
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		u, _ = url.Parse("http://127.0.0.1/platform/settings?section=models")
	}
	query := u.Query()
	query.Set("chatgpt_login", result)
	if accountID != "" {
		query.Set("chatgpt_account", accountID)
	}
	u.RawQuery = query.Encode()
	return u.String()
}

func (h *ChatGPTHandler) Callback(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("Content-Security-Policy", "default-src 'none'; base-uri 'none'; frame-ancestors 'none'")
	for _, key := range []string{"state", "code", "client_id", "error"} {
		if len(c.Request.URL.Query()[key]) > 1 {
			chatGPTFailure(c, chatgpt.ErrInvalidState)
			return
		}
	}
	var account *chatgpt.Account
	var err error
	if c.Query("error") != "" {
		err = h.manager.CancelLogin(c.Request.Context(), c.Query("state"), c.Query("error"))
		if err == nil {
			err = errors.New("chatgpt: authorization declined")
		}
	} else {
		account, err = h.manager.CompleteLogin(c.Request.Context(), c.Query("state"), c.Query("code"), c.Query("client_id"))
	}
	data := map[string]string{}
	if err != nil {
		c.Status(http.StatusBadRequest)
		data["Heading"] = "ChatGPT 登录未完成"
		data["Message"] = "请返回模型设置重试。错误代码：" + chatGPTErrorCode(err)
		data["English"] = "ChatGPT sign-in did not complete. Return to model settings and try again."
		data["SettingsURL"] = chatGPTSettingsURL("error", "")
		var retry *chatgpt.AuthorizationRetryError
		if errors.As(err, &retry) {
			data["Message"] = "授权码已失效。点击 Continue with ChatGPT 使用新的授权码继续登录。"
			data["English"] = "The authorization code is no longer usable. Continue with ChatGPT to start a fresh authorization."
			data["RetryURL"] = chatGPTLaunchURL(retry.CallbackURL, retry.State)
		}
	} else {
		c.Status(http.StatusOK)
		data["Heading"] = "已连接 ChatGPT"
		data["Message"] = "请返回模型设置，选择账号与模型。符合条件的生成请求将使用你的 ChatGPT 订阅额度。"
		data["English"] = "ChatGPT is connected. Return to model settings to select your account and model. Eligible generation requests use your ChatGPT plan."
		data["SettingsURL"] = chatGPTSettingsURL("success", account.ID)
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	_ = chatGPTCallbackPage.Execute(c.Writer, data)
}

func (h *ChatGPTHandler) Accounts(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	accounts, err := h.manager.ListAccounts(c.Request.Context())
	if err != nil {
		chatGPTFailure(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"accounts": accounts, "encryption_ready": utils.GetAESKey() != nil, "callback_url": chatGPTCallbackURL()}})
}

func (h *ChatGPTHandler) Models(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	models, err := h.manager.Models(c.Request.Context(), c.Param("account_id"))
	if err != nil {
		chatGPTFailure(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": models})
}

func (h *ChatGPTHandler) Logout(c *gin.Context) {
	result, err := h.manager.Logout(c.Request.Context(), c.Param("account_id"))
	if err != nil {
		chatGPTFailure(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *ChatGPTHandler) AckWelcome(c *gin.Context) {
	var req struct {
		AccountID string `json:"account_id" binding:"required"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": gin.H{"code": "invalid_request", "message": "invalid_request"}})
		return
	}
	if err := h.manager.AcknowledgeWelcome(c.Request.Context(), req.AccountID); err != nil {
		chatGPTFailure(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"welcome_acknowledged": true}})
}
