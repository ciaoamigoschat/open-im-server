package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openimsdk/open-im-server/v3/internal/moderation"
	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/utils/idutil"
)

type ModerationAPI struct {
	repository *moderation.Repository
}

func NewModerationAPI(repository *moderation.Repository) *ModerationAPI {
	return &ModerationAPI{repository: repository}
}

func ModerationAdminOnly(authClient *rpcli.AuthClient, adminUserIDs []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		operationID := c.GetHeader(constant.OperationID)
		if operationID == "" {
			operationID = idutil.OperationIDGenerator()
		}
		c.Set(constant.OperationID, operationID)
		if c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		if !authverify.IsAppManagerUid(c, adminUserIDs) {
			token := c.GetHeader(constant.Token)
			if token == "" {
				apiresp.GinError(c, servererrs.ErrNoPermission.WrapMsg("admin token required"))
				c.Abort()
				return
			}
			resp, err := authClient.ParseToken(c, token)
			if err != nil {
				apiresp.GinError(c, err)
				c.Abort()
				return
			}
			c.Set(constant.OpUserID, resp.UserID)
			c.Set(constant.OpUserPlatform, constant.PlatformIDToName(int(resp.PlatformID)))
		}
		if err := authverify.CheckAdmin(c, adminUserIDs); err != nil {
			apiresp.GinError(c, err)
			c.Abort()
			return
		}
		c.Next()
	}
}

func (m *ModerationAPI) GetConfig(c *gin.Context) {
	snapshot, err := m.repository.LoadSnapshot(c)
	if err != nil {
		m.respond(c, nil, err)
		return
	}
	apiresp.GinSuccess(c, gin.H{"config": snapshot.Config, "version": snapshot.Version})
}

func (m *ModerationAPI) PutConfig(c *gin.Context) {
	var config moderation.Config
	if err := c.ShouldBindJSON(&config); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WithDetail(err.Error()).Wrap())
		return
	}
	if err := config.Validate(); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WithDetail(err.Error()).Wrap())
		return
	}
	version, err := m.repository.SaveConfig(c, config)
	if err != nil {
		m.respond(c, nil, err)
		return
	}
	snapshot, err := m.repository.LoadSnapshot(c)
	if err != nil {
		m.respond(c, nil, err)
		return
	}
	m.respond(c, gin.H{"config": snapshot.Config, "version": version}, nil)
}

func (m *ModerationAPI) GetBannedWords(c *gin.Context) {
	snapshot, err := m.repository.LoadSnapshot(c)
	if err != nil {
		m.respond(c, nil, err)
		return
	}
	apiresp.GinSuccess(c, snapshot.BannedTerms)
}

func (m *ModerationAPI) PostBannedWord(c *gin.Context) {
	var item moderation.BannedTerm
	if !m.bindBannedTerm(c, &item) {
		return
	}
	result, err := m.repository.SaveBannedTerm(c, item)
	m.respond(c, result, err)
}

func (m *ModerationAPI) PutBannedWord(c *gin.Context) {
	var item moderation.BannedTerm
	if !m.bindBannedTerm(c, &item) {
		return
	}
	item.ID = c.Param("id")
	result, err := m.repository.SaveBannedTerm(c, item)
	m.respond(c, result, err)
}

func (m *ModerationAPI) DeleteBannedWord(c *gin.Context) {
	m.respond(c, gin.H{"id": c.Param("id"), "deleted": true}, m.repository.DeleteBannedTerm(c, c.Param("id")))
}

func (m *ModerationAPI) GetBannedDomains(c *gin.Context) {
	snapshot, err := m.repository.LoadSnapshot(c)
	if err != nil {
		m.respond(c, nil, err)
		return
	}
	apiresp.GinSuccess(c, snapshot.BannedDomains)
}

func (m *ModerationAPI) PostBannedDomain(c *gin.Context) {
	var item moderation.BannedDomain
	if !m.bindBannedDomain(c, &item) {
		return
	}
	result, err := m.repository.SaveBannedDomain(c, item)
	m.respond(c, result, err)
}

func (m *ModerationAPI) PutBannedDomain(c *gin.Context) {
	var item moderation.BannedDomain
	if !m.bindBannedDomain(c, &item) {
		return
	}
	item.ID = c.Param("id")
	result, err := m.repository.SaveBannedDomain(c, item)
	m.respond(c, result, err)
}

func (m *ModerationAPI) DeleteBannedDomain(c *gin.Context) {
	m.respond(c, gin.H{"id": c.Param("id"), "deleted": true}, m.repository.DeleteBannedDomain(c, c.Param("id")))
}

func (m *ModerationAPI) GetUserStatus(c *gin.Context) {
	result, err := m.repository.UserStatus(c, c.Param("userId"))
	m.respond(c, result, err)
}

type restrictionRequest struct {
	DurationSeconds int64  `json:"durationSeconds" binding:"gte=0,lte=31536000"`
	Reason          string `json:"reason" binding:"max=200"`
}

func (m *ModerationAPI) MuteUser(c *gin.Context) {
	var request restrictionRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.DurationSeconds < 1 {
		apiresp.GinError(c, errs.ErrArgs.WithDetail("durationSeconds deve essere positivo").Wrap())
		return
	}
	restriction := moderation.Restriction{Reason: request.Reason, ActorID: mcontext.GetOpUserID(c)}
	err := m.repository.SetMute(c, c.Param("userId"), restriction, time.Duration(request.DurationSeconds)*time.Second)
	if err == nil {
		err = m.auditUserAction(c, moderation.ActionMute, "MANUAL_MUTE", request.DurationSeconds)
	}
	m.respond(c, gin.H{"userID": c.Param("userId"), "muted": true, "durationSeconds": request.DurationSeconds}, err)
}

func (m *ModerationAPI) UnmuteUser(c *gin.Context) {
	err := m.repository.DeleteMute(c, c.Param("userId"))
	if err == nil {
		err = m.auditUserAction(c, moderation.ActionAllow, "MANUAL_UNMUTE", 0)
	}
	m.respond(c, gin.H{"userID": c.Param("userId"), "muted": false}, err)
}

func (m *ModerationAPI) BanUser(c *gin.Context) {
	var request restrictionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WithDetail(err.Error()).Wrap())
		return
	}
	restriction := moderation.Restriction{Reason: request.Reason, ActorID: mcontext.GetOpUserID(c)}
	err := m.repository.SetBan(c, c.Param("userId"), restriction, time.Duration(request.DurationSeconds)*time.Second)
	if err == nil {
		err = m.auditUserAction(c, moderation.ActionTempBan, "MANUAL_BAN", request.DurationSeconds)
	}
	m.respond(c, gin.H{"userID": c.Param("userId"), "banned": true, "durationSeconds": request.DurationSeconds}, err)
}

func (m *ModerationAPI) UnbanUser(c *gin.Context) {
	err := m.repository.DeleteBan(c, c.Param("userId"))
	if err == nil {
		err = m.auditUserAction(c, moderation.ActionAllow, "MANUAL_UNBAN", 0)
	}
	m.respond(c, gin.H{"userID": c.Param("userId"), "banned": false}, err)
}

func (m *ModerationAPI) GetEvents(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	result, err := m.repository.Events(c, moderation.EventFilter{Page: page, PageSize: pageSize, UserID: c.Query("userID"), Action: moderation.Action(c.Query("action")), Reason: c.Query("reason")})
	m.respond(c, result, err)
}

func (m *ModerationAPI) GetStats(c *gin.Context) {
	result, err := m.repository.Stats(c)
	m.respond(c, result, err)
}

func (m *ModerationAPI) bindBannedTerm(c *gin.Context, item *moderation.BannedTerm) bool {
	if err := c.ShouldBindJSON(item); err != nil || strings.TrimSpace(item.Term) == "" || item.Severity < 0 || !validModerationAction(item.Action) {
		apiresp.GinError(c, errs.ErrArgs.WithDetail("banned word non valida").Wrap())
		return false
	}
	item.Term = strings.TrimSpace(item.Term)
	return true
}

func (m *ModerationAPI) bindBannedDomain(c *gin.Context, item *moderation.BannedDomain) bool {
	if err := c.ShouldBindJSON(item); err != nil || strings.TrimSpace(item.Domain) == "" || item.Severity < 0 || !validModerationAction(item.Action) {
		apiresp.GinError(c, errs.ErrArgs.WithDetail("banned domain non valido").Wrap())
		return false
	}
	item.Domain = strings.ToLower(strings.TrimSpace(item.Domain))
	return true
}

func validModerationAction(action moderation.Action) bool {
	switch action {
	case "", moderation.ActionAllow, moderation.ActionAllowLog, moderation.ActionBlock, moderation.ActionMute, moderation.ActionTempBan, moderation.ActionReview:
		return true
	default:
		return false
	}
}

func (m *ModerationAPI) auditUserAction(c *gin.Context, action moderation.Action, reason string, duration int64) error {
	return m.repository.AppendEvent(c, moderation.Event{ID: strconv.FormatInt(time.Now().UnixNano(), 36), UserID: c.Param("userId"), Action: action, Reasons: []string{reason}, DurationSeconds: duration, CreatedAt: time.Now().UTC()})
}

func (m *ModerationAPI) respond(c *gin.Context, data any, err error) {
	if err != nil {
		apiresp.GinError(c, servererrs.ErrInternalServer.WithDetail(err.Error()).Wrap())
		return
	}
	apiresp.GinSuccess(c, data)
}
