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
	"sort"
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
	UserID  common.UserID
	ChatID  int64
	Items   []ResolvedFillItem
	Expires time.Time
	SlotSet map[int64]struct{} // допустимые message_id
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

const fillMaxLinesInMessage = 45

// formatSlotIntervalLocal — интервал слота в локальном времени: от (время сообщения − интервал) до времени сообщения.
func formatSlotIntervalLocal(user db.User, messageTsUTC time.Time, intervalMin int64) string {
	end := UserLocalWallClock(user, messageTsUTC.UTC())
	start := end.Add(-time.Duration(intervalMin) * time.Minute)
	const d = "02.01.2006"
	const t = "15:04"
	if start.Year() == end.Year() && start.YearDay() == end.YearDay() {
		return fmt.Sprintf("%s %s–%s", start.Format(d), start.Format(t), end.Format(t))
	}
	return fmt.Sprintf("%s %s – %s %s", start.Format(d), start.Format(t), end.Format(d), end.Format(t))
}

// buildUnfilledSlotsBulletList — маркированный список интервалов (без активности), для приветствия /fill.
func buildUnfilledSlotsBulletList(user db.User, unfilled []db.ActivityLog, maxLines int) (text string, total int) {
	total = len(unfilled)
	if total == 0 {
		return "", 0
	}
	byMid := make(map[int64]db.ActivityLog, len(unfilled))
	for _, u := range unfilled {
		byMid[u.MessageID] = u
	}
	mids := make([]int64, 0, len(byMid))
	for mid := range byMid {
		mids = append(mids, mid)
	}
	sort.Slice(mids, func(i, j int) bool {
		return byMid[mids[i]].Timestamp.Before(byMid[mids[j]].Timestamp)
	})
	var b strings.Builder
	nShow := len(mids)
	if nShow > maxLines {
		nShow = maxLines
	}
	for i := 0; i < nShow; i++ {
		u := byMid[mids[i]]
		line := formatSlotIntervalLocal(user, u.Timestamp, u.IntervalMinutes)
		b.WriteString("• ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if len(mids) > maxLines {
		b.WriteString(fmt.Sprintf("… и ещё %d слотов.\n", len(mids)-maxLines))
	}
	return b.String(), total
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
		_, _ = tg.Bot.Send(tgbotapi.NewMessage(message.Chat.ID,
			"Нет незаполненных слотов за последние 7 дней — описывать нечего."))
		return
	}

	list, total := buildUnfilledSlotsBulletList(*user, unfilled, fillMaxLinesInMessage)
	intro := fmt.Sprintf(
		"Сейчас не заполнены %d напоминаний. Опиши, чем ты занимался в этих промежутках — текстом или голосовым:\n\n%s\n"+
			"Я сопоставлю ответ со слотами выше и покажу план на подтверждение.\n"+
			"Отмена: /fill_cancel",
		total,
		list,
	)
	if len(intro) > 4000 {
		intro = intro[:3990] + "…"
	}

	common.UserStates[userID] = common.UserState{State: common.InFill, WaitingChannel: nil}
	_, err = tg.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, intro))
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

	// Только немьюченные активности — как в обычных опросах, чтобы LLM не предлагала скрытые ветки.
	isMuted := false
	paths, err := db.GetFullActivities(userID, &isMuted)
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

	if len(items) == 0 {
		_, _ = tg.Bot.Send(tgbotapi.NewMessage(chatID,
			"По описанию не удалось сопоставить ни один слот (или модель ничего не предложила). "+
				"Укажи явнее, какие интервалы времени чем занимался — можно только часть пропуска."))
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

	slotByMID := make(map[int64]db.ActivityLog, len(unfilled))
	for _, u := range unfilled {
		slotByMID[u.MessageID] = u
	}
	midToName := make(map[int64]string)
	for _, it := range items {
		for _, mid := range it.MessageIDs {
			midToName[mid] = it.ActivityName
		}
	}
	// В превью и подтверждении — только слоты, которые реально пойдут в план (частичное заполнение).
	midsSorted := make([]int64, 0, len(midToName))
	for mid := range midToName {
		midsSorted = append(midsSorted, mid)
	}
	sort.Slice(midsSorted, func(i, j int) bool {
		return slotByMID[midsSorted[i]].Timestamp.Before(slotByMID[midsSorted[j]].Timestamp)
	})

	totalSlots := len(midsSorted)
	showMids := midsSorted
	listTruncated := false
	if len(showMids) > fillMaxLinesInMessage {
		showMids = showMids[:fillMaxLinesInMessage]
		listTruncated = true
	}

	var b strings.Builder
	b.WriteString("Будут заполнены только перечисленные слоты; остальные незаполненные напоминания не меняются.\n")
	b.WriteString("Проверь план — каждая строка: интервал слота и выбранная активность:\n")
	for _, mid := range showMids {
		u := slotByMID[mid]
		interval := formatSlotIntervalLocal(*user, u.Timestamp, u.IntervalMinutes)
		b.WriteString("• ")
		b.WriteString(interval)
		b.WriteString(" — «")
		b.WriteString(midToName[mid])
		b.WriteString("»\n")
	}
	if listTruncated {
		b.WriteString(fmt.Sprintf(
			"\n(Показаны первые %d из %d строк плана — лимит длины; при «Подтвердить» всё равно применятся все %d слотов из плана.)\n",
			fillMaxLinesInMessage, totalSlots, totalSlots,
		))
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
		editFillPlanMessageFooter(callback, "\n\n— Отменено, записи не менялись.")
		return
	case "fill__confirm":
		applyFillPlan(callback, entry)
	default:
		return
	}
}

var emptyInlineKeyboard = tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}}

// editFillPlanMessageFooter дописывает итог к сообщению с кнопками и убирает клавиатуру.
func editFillPlanMessageFooter(callback *tgbotapi.CallbackQuery, suffix string) {
	if callback == nil || callback.Message == nil {
		return
	}
	base := callback.Message.Text
	newText := base + suffix
	const tgMax = 4096
	if len(newText) > tgMax {
		maxBase := tgMax - len(suffix) - 4 // «…» + запас
		if maxBase < 200 {
			newText = "…" + suffix
			if len(newText) > tgMax {
				newText = newText[len(newText)-tgMax:]
			}
		} else {
			newText = base[:maxBase] + "…" + suffix
		}
	}
	_, err := tg.Bot.Send(tgbotapi.NewEditMessageTextAndMarkup(
		callback.Message.Chat.ID,
		callback.Message.MessageID,
		newText,
		emptyInlineKeyboard,
	))
	if err != nil {
		log.Printf("editFillPlanMessageFooter: %v", err)
	}
}

func applyFillPlan(callback *tgbotapi.CallbackQuery, entry *pendingFillEntry) {
	if callback == nil || callback.Message == nil {
		return
	}
	for _, it := range entry.Items {
		for _, mid := range it.MessageIDs {
			if err := db.SetActivityLogActivityID(mid, int64(entry.UserID), it.ActivityID); err != nil {
				log.Printf("SetActivityLogActivityID %d: %v", mid, err)
				editFillPlanMessageFooter(callback, fmt.Sprintf("\n\n— Ошибка при сохранении (message_id=%d): %v", mid, err))
				return
			}
			edit := tgbotapi.NewEditMessageTextAndMarkup(
				entry.ChatID, int(mid),
				"Saved activity \""+it.ActivityName+"\"",
				emptyInlineKeyboard,
			)
			if _, err := tg.Bot.Send(edit); err != nil {
				log.Printf("edit message %d: %v (возможно старше 48ч)", mid, err)
			}
		}
	}

	n := countSlots(entry.Items)
	editFillPlanMessageFooter(callback, fmt.Sprintf("\n\n— Подтверждено. Заполнено слотов: %d.", n))
}

func countSlots(items []ResolvedFillItem) int {
	n := 0
	for _, it := range items {
		n += len(it.MessageIDs)
	}
	return n
}
