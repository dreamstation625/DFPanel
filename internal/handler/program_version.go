package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"dfpanel/internal/config"
	"dfpanel/internal/database"
	"dfpanel/internal/model"
	"dfpanel/internal/versioncheck"
)

type programVersionStatus struct {
	Current         string `json:"current"`
	Latest          string `json:"latest"`
	State           string `json:"state"` // current / update / ahead / unknown / error
	UpdateAvailable bool   `json:"updateAvailable"`
	ReleaseURL      string `json:"releaseUrl,omitempty"`
	Error           string `json:"error,omitempty"`
}

type agentProgramVersionStatus struct {
	ID     uint                 `json:"id"`
	Status programVersionStatus `json:"status"`
}

func compareProgramVersion(current, latest, releaseURL, checkError string) programVersionStatus {
	status := programVersionStatus{Current: current, Latest: latest, ReleaseURL: releaseURL}
	if checkError != "" {
		status.State = "error"
		status.Error = checkError
		return status
	}
	order, valid := versioncheck.Compare(current, latest)
	if !valid {
		status.State = "unknown"
		return status
	}
	switch {
	case order < 0:
		status.State = "update"
		status.UpdateAvailable = true
	case order > 0:
		status.State = "ahead"
	default:
		status.State = "current"
	}
	return status
}

func publishedFor(current string, latest versioncheck.Latest) versioncheck.Published {
	isPrerelease, valid := versioncheck.IsPrerelease(current)
	if valid && !isPrerelease {
		return latest.Stable
	}
	return latest.Any
}

// ProgramVersionCheck 比较运行中的面板和每个 Agent 与最新发布版本，不触发升级。
func ProgramVersionCheck(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		latest, fetchErr := versioncheck.Default.Check(c.Request.Context(), c.Query("refresh") == "true")
		panelError := ""
		if fetchErr != nil {
			panelError = fetchErr.Error()
		}
		panelVersion := "dev"
		if cfg != nil && cfg.Version != "" {
			panelVersion = cfg.Version
		}
		panelPublished := publishedFor(panelVersion, latest)
		panel := compareProgramVersion(panelVersion, panelPublished.PanelVersion, panelPublished.ReleaseURL, panelError)

		var agents []model.Agent
		if err := database.DB.Select("id", "version").Order("id asc").Find(&agents).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "读取 Agent 版本失败"})
			return
		}
		agentStatuses := make([]agentProgramVersionStatus, 0, len(agents))
		for _, agent := range agents {
			published := publishedFor(agent.Version, latest)
			agentError := published.AgentError
			if fetchErr != nil {
				agentError = fetchErr.Error()
			}
			agentStatuses = append(agentStatuses, agentProgramVersionStatus{
				ID:     agent.ID,
				Status: compareProgramVersion(agent.Version, published.AgentVersion, published.AgentReleaseURL, agentError),
			})
		}
		c.JSON(http.StatusOK, gin.H{"panel": panel, "agents": agentStatuses, "checkedAt": latest.CheckedAt})
	}
}
