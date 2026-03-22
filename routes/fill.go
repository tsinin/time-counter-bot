package routes

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"TimeCounterBot/common"
	"TimeCounterBot/db"
	"TimeCounterBot/services"
	tg "TimeCounterBot/tg/bot"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const unfilledSlotsLookback = 7 * 24 * time.Hour

// ResolvedFillItem готовые к применению назначения после разрешения путей в leaf id.
type ResolvedFillItem struct {
	MessageIDs   []int64
	ActivityID   int64
	ActivityName string
}

type pendingFillEntry struct {
	UserID   common.UserID
	ChatID   int64
	Items    []ResolvedFillItem
	Expires  time.Time
	SlotSet  map[int64]struct{} // допустимые message_id
}

var (
	pendingFillsMu sync.Mutex
	pendingFills   = make(map[string]*pendingFillEntry)
)

func randomFillSessionID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func openAIKey() string {
	return strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
}

// FillCommand начинает сценарий /fill.
func FillCommand(message *tgbotapi.Message) {
	tgUser := message.From
	if tgUser == nil {
		return
	}
	userID := common.UserID(tgUser.ID)

	st := common.UserStates[userID]
	if st.State == common.InCommand {
		_, _ = tg.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "Сначала заверши текущую команду (например ввод активности)."))
		return
	}
	if st.State == common.InFill {
		_, _ = tg.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "Уже жду описание для /fill. Или отправь /fill_cancel."))
		return
	}
	if openAIKey() == "" {
		_, _ = tg.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "Команда /fill недоступна: не задан OPENAI_API_KEY на сервере."))
		return
	}

	common.UserStates[userID] = common.UserState{State: common.InFill, WaitingChannel: nil}
	_, err := tg.Bot.Send(tgbotapi.NewMessage(message.Chat.ID,
		"Опиши текстом или голосовым, чем ты занимался в пропущенных слотах. "+
			"Я сопоставлю это с незаполненными напоминаниями за последние 7 дней и покажу план.\n"+
			"Отмена: /fill_cancel"))
	if err != nil {
		log.Fatal(err)
	}
}

// FillCancelCommand сбрасывает режим /fill.
func FillCancelCommand(message *tgbotapi.Message) {
	if message.From == nil {
		return
	}
	userID := common.UserID(message.From.ID)
	st := common.UserStates[userID]
	if st.State != common.InFill {
		_, _ = tg.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "Ты не в режиме /fill."))
		return
	}
	common.UserStates[userID] = common.UserState{State: common.Idle, WaitingChannel: nil}
	_, _ = tg.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "Ок, режим заполнения отменён."))
}

// HandleFillText обрабатывает текст в режиме InFill.
func HandleFillText(message *tgbotapi.Message) {
	if message.From == nil {
		return
	}
	userID := common.UserID(message.From.ID)
	if common.UserStates[userID].State != common.InFill {
		return
	}
	text := strings.TrimSpace(message.Text)
	if text == "" {
		return
	}
	runFillWithDescription(message.Chat.ID, userID, text)
}

// HandleFillVoice загружает голосовое и передаёт текст в тот же пайплайн, что и /fill.
func HandleFillVoice(message *tgbotapi.Message) {
	if message.From == nil || message.Voice == nil {
		return
	}
	userID := common.UserID(message.From.ID)
	if common.UserStates[userID].State != common.InFill {
		return
	}
	if openAIKey() == "" {
		common.UserStates[userID] = common.UserState{State: common.Idle, WaitingChannel: nil}
		_, _ = tg.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "OPENAI_API_KEY не задан."))
		return
	}

	file, err := tg.Bot.GetFile(tgbotapi.FileConfig{FileID: message.Voice.FileID})
	if err != nil {
		log.Printf("GetFile voice: %v", err)
		_, _ = tg.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "Не удалось получить файл голосового."))
		return
	}
	url := file.Link(tg.Bot.Token)

	tmp, err := os.CreateTemp("", "tg-voice-*.oga")
	if err != nil {
		log.Fatal(err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath)

	resp, err := http.Get(url)
	if err != nil {
		log.Printf("download voice: %v", err)
		_, _ = tg.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "Не удалось скачать голосовое."))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = tg.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "Ошибка скачивания голосового."))
		return
	}
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatal(err)
	}
	_, err = io.Copy(f, resp.Body)
	_ = f.Close()
	if err != nil {
		_, _ = tg.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "Ошибка сохранения голосового."))
		return
	}

	ctx, cancelCtx := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancelCtx()

	text, err := services.TranscribeOggVoice(ctx, services.NewOpenAIClient(openAIKey()), tmpPath)
	if err != nil {
		log.Printf("whisper: %v", err)
		_, _ = tg.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "Не удалось расшифровать голосовое: "+err.Error()))
		return
	}
	if text == "" {
		_, _ = tg.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "Расшифровка пустая, попробуй ещё раз."))
		return
	}
	_, _ = tg.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "Расшифровка: "+text))
	runFillWithDescription(message.Chat.ID, userID, text)
}

func runFillWithDescription(chatID int64, userID common.UserID, description string) {
	defer func() {
		common.UserStates[userID] = common.UserState{State: common.Idle, WaitingChannel: nil}
	}()

	user, err := db.GetUserByID(userID)
	if err != nil {
		log.Fatal(err)
	}

	since := time.Now().Add(-unfilledSlotsLookback)
	unfilled, err := db.GetUnfilledActivityLogs(userID, since)
	if err != nil {
		log.Fatal(err)
	}
	if len(unfilled) == 0 {
		_, _ = tg.Bot.Send(tgbotapi.NewMessage(chatID, "Нет незаполненных слотов за последние 7 дней."))
		return
	}

	paths, err := db.GetFullActivities(userID, nil)
	if err != nil {
		log.Fatal(err)
	}
	if len(paths) == 0 {
		_, _ = tg.Bot.Send(tgbotapi.NewMessage(chatID, "У тебя нет активностей. Сначала добавь через /register_new_activity."))
		return
	}
	pathStrs := make([]string, 0, len(paths))
	for _, p := range paths {
		pathStrs = append(pathStrs, p.Name)
	}

	slots := make([]services.FillSlot, 0, len(unfilled))
	slotSet := make(map[int64]struct{})
	for _, u := range unfilled {
		local := UserLocalWallClock(*user, u.Timestamp.UTC())
		slots = append(slots, services.FillSlot{
			MessageID:       u.MessageID,
			LocalTime:       local.Format("15:04"),
			IntervalMinutes: u.IntervalMinutes,
		})
		slotSet[u.MessageID] = struct{}{}
	}

	ctx, cancelCtx := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancelCtx()

	client := services.NewOpenAIClient(openAIKey())
	raw, err := services.ParseFillAssignments(ctx, client, description, slots, pathStrs)
	if err != nil {
		log.Printf("fill llm: %v", err)
		_, _ = tg.Bot.Send(tgbotapi.NewMessage(chatID, "Не удалось разобрать описание: "+err.Error()))
		return
	}

	var items []ResolvedFillItem
	seen := make(map[int64]struct{})
	for _, a := range raw {
		leafID, err := db.ResolveLeafIDByActivityPath(userID, a.ActivityPath)
		if err != nil {
			_, _ = tg.Bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Не нашёл активность по пути из ответа модели: %q (%v)", a.ActivityPath, err)))
			return
		}
		acts, err := db.GetSimpleActivities(userID, nil, nil)
		if err != nil {
			log.Fatal(err)
		}
		name, err := db.BuildFullActivityName(acts, leafID)
		if err != nil {
			name = a.ActivityPath
		}
		for _, mid := range a.MessageIDs {
			if _, ok := slotSet[mid]; !ok {
				_, _ = tg.Bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Недопустимый message_id в плане: %d", mid)))
				return
			}
			if _, dup := seen[mid]; dup {
				_, _ = tg.Bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Слот %d встречается в плане дважды.", mid)))
				return
			}
			seen[mid] = struct{}{}
		}
		if len(a.MessageIDs) == 0 {
			continue
		}
		items = append(items, ResolvedFillItem{
			MessageIDs:   append([]int64(nil), a.MessageIDs...),
			ActivityID:   leafID,
			ActivityName: name,
		})
	}

	for mid := range slotSet {
		if _, ok := seen[mid]; !ok {
			_, _ = tg.Bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("В плане не хватает слота message_id=%d. Попробуй описать явнее.", mid)))
			return
		}
	}

	if len(items) == 0 {
		_, _ = tg.Bot.Send(tgbotapi.NewMessage(chatID, "Модель вернула пустой план."))
		return
	}

	sid := randomFillSessionID()
	pendingFillsMu.Lock()
	pendingFills[sid] = &pendingFillEntry{
		UserID:  userID,
		ChatID:  chatID,
		Items:   items,
		Expires: time.Now().Add(30 * time.Minute),
		SlotSet: slotSet,
	}
	pendingFillsMu.Unlock()

	var b strings.Builder
	b.WriteString("Вот что я понял:\n")
	for _, it := range items {
		b.WriteString(fmt.Sprintf("- %d слот(ов) → \"%s\"\n", len(it.MessageIDs), it.ActivityName))
	}
	b.WriteString("\nПодтвердить или отменить?")

	confirmData := "fill__confirm " + sid
	cancelData := "fill__cancel " + sid
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.InlineKeyboardButton{Text: "Подтвердить", CallbackData: &confirmData},
			tgbotapi.InlineKeyboardButton{Text: "Отменить", CallbackData: &cancelData},
		),
	)
	msg := tgbotapi.NewMessage(chatID, b.String())
	msg.ReplyMarkup = kb
	if _, err := tg.Bot.Send(msg); err != nil {
		log.Fatal(err)
	}
}

// HandleFillCallback обрабатывает fill__confirm / fill__cancel.
func HandleFillCallback(callback *tgbotapi.CallbackQuery) {
	parts := strings.SplitN(callback.Data, " ", 3)
	if len(parts) < 2 {
		return
	}
	action, sid := parts[0], parts[1]

	pendingFillsMu.Lock()
	entry, ok := pendingFills[sid]
	if ok && time.Now().After(entry.Expires) {
		delete(pendingFills, sid)
		ok = false
	}
	if ok {
		delete(pendingFills, sid)
	}
	pendingFillsMu.Unlock()

	_, _ = tg.Bot.Request(tgbotapi.NewCallback(callback.ID, ""))

	if !ok {
		_, _ = tg.Bot.Send(tgbotapi.NewMessage(callback.Message.Chat.ID, "Сессия подтверждения устарела. Запусти /fill снова."))
		return
	}
	if common.UserID(callback.From.ID) != entry.UserID {
		_, _ = tg.Bot.Send(tgbotapi.NewMessage(callback.Message.Chat.ID, "Это не твоя сессия."))
		return
	}

	switch action {
	case "fill__cancel":
		_, _ = tg.Bot.Send(tgbotapi.NewMessage(callback.Message.Chat.ID, "Отменено, записи не менял."))
		return
	case "fill__confirm":
		applyFillPlan(callback, entry)
	default:
		return
	}
}

func applyFillPlan(callback *tgbotapi.CallbackQuery, entry *pendingFillEntry) {
	for _, it := range entry.Items {
		for _, mid := range it.MessageIDs {
			if err := db.SetActivityLogActivityID(mid, int64(entry.UserID), it.ActivityID); err != nil {
				log.Printf("SetActivityLogActivityID %d: %v", mid, err)
				_, _ = tg.Bot.Send(tgbotapi.NewMessage(entry.ChatID, fmt.Sprintf("Ошибка БД для message_id=%d", mid)))
				return
			}
			edit := tgbotapi.NewEditMessageTextAndMarkup(
				entry.ChatID, int(mid),
				"Saved activity \""+it.ActivityName+"\"",
				tgbotapi.InlineKeyboardMarkup{InlineKeyboard: make([][]tgbotapi.InlineKeyboardButton, 0)},
			)
			if _, err := tg.Bot.Send(edit); err != nil {
				log.Printf("edit message %d: %v (возможно старше 48ч)", mid, err)
			}
		}
	}

	_, _ = tg.Bot.Send(tgbotapi.NewMessage(entry.ChatID, fmt.Sprintf("Готово: обновлено слотов: %d.", countSlots(entry.Items))))

	// убираем клавиатуру у сообщения с кнопками
	if callback.Message != nil {
		strip := tgbotapi.NewEditMessageTextAndMarkup(
			callback.Message.Chat.ID, callback.Message.MessageID,
			callback.Message.Text,
			tgbotapi.InlineKeyboardMarkup{InlineKeyboard: make([][]tgbotapi.InlineKeyboardButton, 0)},
		)
		_, _ = tg.Bot.Send(strip)
	}
}

func countSlots(items []ResolvedFillItem) int {
	n := 0
	for _, it := range items {
		n += len(it.MessageIDs)
	}
	return n
}
