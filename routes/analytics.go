package routes

import (
	"TimeCounterBot/common"
	tg "TimeCounterBot/tg/bot"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"TimeCounterBot/db"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// ActivityNode — структура узла активности
type ActivityNode struct {
	ID       int      `json:"id"`
	Name     string   `json:"name"`
	ParentID *int     `json:"parent_id"`
	Duration *float64 `json:"duration"`
}

// ActivityData — структура для передачи в скрипт
type ActivityData struct {
	Nodes []ActivityNode `json:"nodes"`
}

// getUserActivityDataForInterval собирает данные активности
// для пользователя user за интервал [start, end].
func getUserActivityDataForInterval(user db.User, start, end time.Time) ActivityData {
	var data ActivityData

	// Получаем все активности пользователя.
	activities := db.GetSimpleActivities(user.ID)

	logDurations := db.GetLogDurations(user.ID, start, end)

	// Преобразуем полученные активности в ActivityNode.
	for _, act := range activities {
		var node ActivityNode
		// Приводим тип id к int.
		node.ID = int(act.ID)
		node.Name = act.Name
		// Если ParentActivityID равен -1, значит это корень.
		if act.ParentActivityID == -1 {
			node.ParentID = nil
		} else {
			pid := int(act.ParentActivityID)
			node.ParentID = &pid
		}
		// Если активность — листовая, задаем длительность.
		if act.IsLeaf {
			dur, ok := logDurations[act.ID]
			if !ok {
				dur = 0
			}
			node.Duration = &dur
		} else {
			node.Duration = nil
		}
		data.Nodes = append(data.Nodes, node)
	}
	return data
}

// GetDayStatisticsCommand вызывается, когда пользователь запрашивает статистику
func GetDayStatisticsCommand(message *tgbotapi.Message) {
	user, err := db.GetUserByID(common.UserID(message.From.ID))
	if err != nil {
		log.Fatal(err)
	}

	spl := strings.Split(message.Text, " ")
	start, err := time.Parse(time.RFC3339, spl[1])
	if err != nil {
		log.Fatal(err)
	}
	end, err := time.Parse(time.RFC3339, spl[2])
	if err != nil {
		log.Fatal(err)
	}

	data := getUserActivityDataForInterval(*user, start, end)
	// data := ActivityData{
	// 	Nodes: []ActivityNode{
	// 		{ID: 1, Name: "Work", ParentID: nil, Duration: nil},
	// 		{ID: 2, Name: "Sleep", ParentID: nil, Duration: FloatPtr(6)},
	// 		{ID: 3, Name: "Leisure", ParentID: nil, Duration: nil},
	// 		{ID: 4, Name: "Exercise", ParentID: nil, Duration: FloatPtr(2)},
	// 		{ID: 5, Name: "Breakfast", ParentID: IntPtr(1), Duration: FloatPtr(2)},
	// 		{ID: 6, Name: "Programming", ParentID: IntPtr(1), Duration: nil},
	// 		{ID: 7, Name: "Golang", ParentID: IntPtr(6), Duration: FloatPtr(4)},
	// 		{ID: 8, Name: "Python", ParentID: IntPtr(6), Duration: FloatPtr(2)},
	// 		{ID: 9, Name: "Gaming", ParentID: IntPtr(3), Duration: FloatPtr(5)},
	// 	},
	// }

	// Кодируем данные в JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		log.Fatalf("Ошибка кодирования JSON: %v", err)
	}

	// Путь к Python-скрипту и файлу вывода
	scriptPath := "python_scripts/generate_sunburst_chart.py"
	outputFile := "pie_chart.png"

	// Логирование перед запуском
	log.Printf("📌 Запускаем Python-скрипт: %s", scriptPath)
	log.Printf("📌 Данные для передачи: %s", string(jsonData))
	log.Printf("📌 Файл для вывода: %s", outputFile)

	// Создаём команду для запуска Python-скрипта
	cmd := exec.Command("python3", scriptPath, string(jsonData), outputFile)

	// Перенаправляем stderr, чтобы увидеть ошибки при выполнении
	cmd.Stderr = os.Stderr

	// Запускаем команду и проверяем ошибки
	err = cmd.Run()
	if err != nil {
		log.Fatalf("❌ Ошибка выполнения скрипта: %v", err)
	}

	// Проверяем, создался ли файл
	if _, err := os.Stat(outputFile); os.IsNotExist(err) {
		log.Fatalf("❌ Файл с графиком не найден: %s", outputFile)
	}

	// Отправляем картинку в Telegram
	msgconf := tgbotapi.NewPhoto(int64(message.Chat.ID), tgbotapi.FilePath(outputFile))
	_, err = tg.Bot.Send(msgconf)
	if err != nil {
		log.Fatalf("❌ Ошибка отправки изображения: %v", err)
	}
}

// FloatPtr — вспомогательная функция для создания указателя на float64
func FloatPtr(value float64) *float64 {
	return &value
}

// IntPtr — вспомогательная функция для создания указателя на int
func IntPtr(value int) *int {
	return &value
}
