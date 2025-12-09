package handlers

import (
	"fmt"
	"log"
	"os"
	"time"

	"yotei-backend/database"
	"yotei-backend/models"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/gorilla/feeds"
)

type EventSettingsRequest struct {
	AllowSettingChanges   bool   `json:"allow_setting_changes"`
	DeadlineEnable        bool   `json:"deadline_enable"`
	Deadline              string `json:"deadline"` // ISO 8601形式
	AutoDecisionEnable    bool   `json:"auto_decision_enable"`
	AutoDecisionThreshold int    `json:"auto_decision_threshold"`
	RSSEnabled            bool   `json:"rss_enabled"`
}

type CreateEventRequest struct {
	Title          string               `json:"title" validate:"required"`
	Description    string               `json:"description"`
	CreatorName    string               `json:"creator_name"`
	CandidateDates []string             `json:"candidate_dates" validate:"required,min=1"` // ISO 8601形式の日時文字列の配列
	Settings       EventSettingsRequest `json:"settings"`
}

type CreateEventResponse struct {
	ID string `json:"id"`
}

type CandidateDateIDRequest struct {
	ID uint `json:"id"`
}

type RegisterParticipantRequest struct {
	EventID                   string                   `json:"event_id" validate:"required"`
	ParticipantID             uint                     `json:"participant_id" validate:"required"`
	Name                      string                   `json:"name" validate:"required"`
	AvailableCandidateDates   []CandidateDateIDRequest `json:"available_candidate_dates" validate:"required"`
	UnavailableCandidateDates []CandidateDateIDRequest `json:"unavailable_candidate_dates" validate:"required"`
}

func CreateEvent(c *fiber.Ctx) error {
	var req CreateEventRequest

	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request format",
		})
	}

	if req.Title == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Title is empty",
		})
	}

	if len(req.CandidateDates) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "No candidate dates provided",
		})
	}

	eventID := uuid.New().String()

	var candidateDates []models.CandidateDate
	for _, dateStr := range req.CandidateDates {
		parsedTime, err := time.Parse(time.RFC3339, dateStr)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid candidate date format. Please use ISO 8601 format",
			})
		}

		candidateDates = append(candidateDates, models.CandidateDate{
			EventID:  eventID,
			DateTime: parsedTime,
		})
	}

	var deadline *time.Time
	if req.Settings.DeadlineEnable && req.Settings.Deadline != "" {
		parsedDeadline, err := time.Parse(time.RFC3339, req.Settings.Deadline)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid deadline format. Please use ISO 8601 format",
			})
		}
		deadline = &parsedDeadline
	}

	event := models.Event{
		ID:                    eventID,
		Title:                 req.Title,
		Description:           req.Description,
		CreatorName:           req.CreatorName,
		AllowSettingChanges:   req.Settings.AllowSettingChanges,
		DeadlineEnable:        req.Settings.DeadlineEnable,
		Deadline:              deadline,
		AutoDecisionEnable:    req.Settings.AutoDecisionEnable,
		AutoDecisionThreshold: req.Settings.AutoDecisionThreshold,
		RSSEnabled:            req.Settings.RSSEnabled,
		CandidateDates:        candidateDates,
	}

	if err := database.DB.Create(&event).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create event",
		})
	}

	response := CreateEventResponse{
		ID: event.ID,
	}

	return c.Status(fiber.StatusCreated).JSON(response)
}

func GetEvent(c *fiber.Ctx) error {
	eventID := c.Params("id")

	var event models.Event
	if err := database.DB.
		Preload("CandidateDates").
		Preload("CandidateDates.Responses").
		Preload("Participants").
		Preload("Participants.Responses").
		First(&event, "id = ?", eventID).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "Event not found",
		})
	}

	return c.JSON(event)
}

func RegisterParticipant(c *fiber.Ctx) error {
	eventID := c.Params("id")
	var req RegisterParticipantRequest

	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request format",
		})
	}

	var event models.Event
	if err := database.DB.Preload("Participants").First(&event, "id = ?", eventID).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "Event not found",
		})
	}

	var responses []models.Response
	for _, candidateDate := range req.AvailableCandidateDates {
		responses = append(responses, models.Response{
			ParticipantID:   req.ParticipantID,
			CandidateDateID: candidateDate.ID,
			Status:          "available",
		})
	}

	for _, candidateDate := range req.UnavailableCandidateDates {
		responses = append(responses, models.Response{
			ParticipantID:   req.ParticipantID,
			CandidateDateID: candidateDate.ID,
			Status:          "unavailable",
		})
	}

	participant := models.Participant{
		ID:        req.ParticipantID,
		EventID:   eventID,
		Name:      req.Name,
		Responses: responses,
	}

	if err := database.DB.Create(&participant).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to register participant",
		})
	}

	log.Println("Participants:", len(event.Participants)+1)
	log.Println("AutoDecisionThreshold:", event.AutoDecisionThreshold)
	log.Println("AutoDecisionEnable:", event.AutoDecisionEnable)
	if event.AutoDecisionEnable && len(event.Participants)+1 >= event.AutoDecisionThreshold {
		if err := CheckAutoDecisionAndFinalize(eventID); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to check and finalize auto decision",
			})
		}
	}

	return c.Status(fiber.StatusCreated).JSON(participant)
}

func UpdateEventSettings(c *fiber.Ctx) error {
	eventID := c.Params("id")
	var req EventSettingsRequest

	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request format",
		})
	}

	var event models.Event
	if err := database.DB.First(&event, "id = ?", eventID).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "Event not found",
		})
	}

	if !event.AllowSettingChanges {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "This event's settings cannot be changed",
		})
	}

	var deadline *time.Time
	if req.DeadlineEnable && req.Deadline != "" {
		parsedDeadline, err := time.Parse(time.RFC3339, req.Deadline)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid deadline format. Please use ISO 8601 format",
			})
		}
		deadline = &parsedDeadline
	}

	if req.DeadlineEnable && event.Deadline != deadline {
		event.DeadlineReached = false
	}

	if req.AutoDecisionEnable && event.AutoDecisionThreshold != req.AutoDecisionThreshold {
		event.AutoDecisionReached = false
	}

	event.AllowSettingChanges = req.AllowSettingChanges
	event.DeadlineEnable = req.DeadlineEnable
	event.Deadline = deadline
	event.AutoDecisionEnable = req.AutoDecisionEnable
	event.AutoDecisionThreshold = req.AutoDecisionThreshold
	event.RSSEnabled = req.RSSEnabled

	if err := database.DB.Save(&event).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to update settings",
		})
	}

	return c.JSON(fiber.Map{
		"message": "Settings updated",
	})
}

func EventRSS(c *fiber.Ctx) error {
	eventID := c.Params("id")
	var event models.Event
	if err := database.DB.First(&event, "id = ?", eventID).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "Event not found",
		})
	}

	feed := &feeds.Feed{
		Title:       fmt.Sprintf("%s", event.Title),
		Link:        &feeds.Link{Href: fmt.Sprintf("%s/%d/vote", os.Getenv("FRONTEND_URL"), event.ID)}, // TODO: 本番環境のURLに変更
		Description: "このイベントの予定日が決定次第、通知が届きます。",
		Created:     time.Now(), // (実際にはイベントの作成日時など)
	}

	var rssFeeds []models.RSSFeed
	if err := database.DB.Where("event_id = ?", eventID).Find(&rssFeeds).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get RSS feeds",
		})
	}

	for _, rssFeed := range rssFeeds {
		feed.Items = append(feed.Items, &feeds.Item{
			Title:       rssFeed.Title,
			Link:        &feeds.Link{Href: rssFeed.Link},
			Description: rssFeed.Description,
			Created:     rssFeed.CreatedAt,
		})
	}

	rss, err := feed.ToRss()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to generate RSS feed",
		})
	}
	return c.Status(fiber.StatusOK).SendString(rss)
}
