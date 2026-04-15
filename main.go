package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Todo struct {
	ID        int       `json:"id"`
	Text      string    `json:"text"`
	Done      bool      `json:"done"`
	CreatedAt time.Time `json:"created_at"`
}

type Storage struct {
	Todos []Todo `json:"todos"`
}

var storeMu sync.Mutex

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printHelp()
		return nil
	}

	path, err := dataPath()
	if err != nil {
		return err
	}

	s, err := load(path)
	if err != nil {
		return err
	}

	switch args[0] {
	case "add":
		if len(args) < 2 {
			return errors.New("укажите текст задачи: todo add \"Купить молоко\"")
		}
		text := strings.TrimSpace(strings.Join(args[1:], " "))
		if text == "" {
			return errors.New("текст задачи не может быть пустым")
		}
		nextID := nextID(s.Todos)
		s.Todos = append(s.Todos, Todo{ID: nextID, Text: text, CreatedAt: time.Now()})
		if err := save(path, s); err != nil {
			return err
		}
		fmt.Printf("Добавлено: [%d] %s\n", nextID, text)

	case "list":
		if len(s.Todos) == 0 {
			fmt.Println("Список задач пуст")
			return nil
		}
		for _, t := range s.Todos {
			mark := " "
			if t.Done {
				mark = "x"
			}
			fmt.Printf("[%s] %d. %s\n", mark, t.ID, t.Text)
		}

	case "done":
		if len(args) != 2 {
			return errors.New("использование: todo done <id>")
		}
		id, err := strconv.Atoi(args[1])
		if err != nil {
			return errors.New("id должен быть числом")
		}
		if !setDone(s, id, true) {
			return fmt.Errorf("задача с id=%d не найдена", id)
		}
		if err := save(path, s); err != nil {
			return err
		}
		fmt.Printf("Задача %d отмечена как выполненная\n", id)

	case "undone":
		if len(args) != 2 {
			return errors.New("использование: todo undone <id>")
		}
		id, err := strconv.Atoi(args[1])
		if err != nil {
			return errors.New("id должен быть числом")
		}
		if !setDone(s, id, false) {
			return fmt.Errorf("задача с id=%d не найдена", id)
		}
		if err := save(path, s); err != nil {
			return err
		}
		fmt.Printf("Задача %d отмечена как невыполненная\n", id)

	case "rm":
		if len(args) != 2 {
			return errors.New("использование: todo rm <id>")
		}
		id, err := strconv.Atoi(args[1])
		if err != nil {
			return errors.New("id должен быть числом")
		}
		if !removeTodo(s, id) {
			return fmt.Errorf("задача с id=%d не найдена", id)
		}
		if err := save(path, s); err != nil {
			return err
		}
		fmt.Printf("Задача %d удалена\n", id)

	case "clear":
		s.Todos = nil
		if err := save(path, s); err != nil {
			return err
		}
		fmt.Println("Все задачи удалены")

	case "help", "-h", "--help":
		printHelp()

	case "web":
		port := "8080"
		if len(args) == 2 {
			port = args[1]
		}
		return runWeb(path, s, port)

	default:
		printHelp()
		return fmt.Errorf("неизвестная команда: %s", args[0])
	}

	return nil
}

func printHelp() {
	fmt.Println("Терминальная TODO на Go")
	fmt.Println("\nКоманды:")
	fmt.Println("  add <текст>     Добавить задачу")
	fmt.Println("  list            Показать задачи")
	fmt.Println("  done <id>       Отметить как выполненную")
	fmt.Println("  undone <id>     Снять отметку выполнения")
	fmt.Println("  rm <id>         Удалить задачу")
	fmt.Println("  clear           Удалить все задачи")
	fmt.Println("  web [порт]      Запустить веб-версию (по умолчанию 8080)")
	fmt.Println("  help            Показать помощь")
}

const pageTpl = `<!doctype html>
<html lang="ru">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width,initial-scale=1" />
  <title>TODO на Go</title>
  <style>
    body { font-family: system-ui, sans-serif; max-width: 760px; margin: 2rem auto; padding: 0 1rem; }
    h1 { margin-top: 0; }
    form { display: flex; gap: .5rem; margin-bottom: 1rem; }
    input[type=text] { flex: 1; padding: .6rem; }
    button { padding: .6rem .8rem; cursor: pointer; }
    ul { list-style: none; padding: 0; }
    li { display: flex; align-items: center; justify-content: space-between; gap: .5rem; padding: .6rem 0; border-bottom: 1px solid #e8e8e8; }
    .done { text-decoration: line-through; color: #777; }
    .actions { display: flex; gap: .5rem; }
    .muted { color: #777; font-size: .9rem; }
  </style>
</head>
<body>
  <h1>Тудушка на Go</h1>
  <p class="muted">Хранилище: {{.Path}}</p>
  <form method="post" action="/add">
    <input type="text" name="text" placeholder="Новая задача..." required />
    <button type="submit">Добавить</button>
  </form>
  {{if .Todos}}
  <ul>
    {{range .Todos}}
    <li>
      <span class="{{if .Done}}done{{end}}">#{{.ID}} {{.Text}}</span>
      <div class="actions">
        <form method="post" action="/toggle">
          <input type="hidden" name="id" value="{{.ID}}" />
          <button type="submit">{{if .Done}}Вернуть{{else}}Готово{{end}}</button>
        </form>
        <form method="post" action="/delete">
          <input type="hidden" name="id" value="{{.ID}}" />
          <button type="submit">Удалить</button>
        </form>
      </div>
    </li>
    {{end}}
  </ul>
  {{else}}
  <p>Пока задач нет.</p>
  {{end}}
  <form method="post" action="/clear">
    <button type="submit">Очистить всё</button>
  </form>
</body>
</html>`

func runWeb(path string, s *Storage, port string) error {
	tpl, err := template.New("page").Parse(pageTpl)
	if err != nil {
		return fmt.Errorf("не удалось подготовить шаблон страницы: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		storeMu.Lock()
		data := struct {
			Todos []Todo
			Path  string
		}{
			Todos: append([]Todo(nil), s.Todos...),
			Path:  path,
		}
		storeMu.Unlock()

		if err := tpl.Execute(w, data); err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("/add", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		text := strings.TrimSpace(r.FormValue("text"))
		if text == "" {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		storeMu.Lock()
		s.Todos = append(s.Todos, Todo{
			ID:        nextID(s.Todos),
			Text:      text,
			CreatedAt: time.Now(),
		})
		_ = save(path, s)
		storeMu.Unlock()
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	mux.HandleFunc("/toggle", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id, err := strconv.Atoi(r.FormValue("id"))
		if err == nil {
			storeMu.Lock()
			for i := range s.Todos {
				if s.Todos[i].ID == id {
					s.Todos[i].Done = !s.Todos[i].Done
					break
				}
			}
			_ = save(path, s)
			storeMu.Unlock()
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	mux.HandleFunc("/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id, err := strconv.Atoi(r.FormValue("id"))
		if err == nil {
			storeMu.Lock()
			_ = removeTodo(s, id)
			_ = save(path, s)
			storeMu.Unlock()
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	mux.HandleFunc("/clear", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		storeMu.Lock()
		s.Todos = nil
		_ = save(path, s)
		storeMu.Unlock()
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	addr := ":" + port
	fmt.Printf("Веб-версия запущена: http://localhost%s\n", addr)
	return http.ListenAndServe(addr, mux)
}

func dataPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("не удалось определить домашнюю директорию: %w", err)
	}
	return filepath.Join(home, ".todo_cli.json"), nil
}

func load(path string) (*Storage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Storage{}, nil
		}
		return nil, fmt.Errorf("не удалось прочитать файл: %w", err)
	}

	if len(data) == 0 {
		return &Storage{}, nil
	}

	var s Storage
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("поврежденный файл данных: %w", err)
	}
	return &s, nil
}

func save(path string, s *Storage) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("не удалось сериализовать данные: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("не удалось сохранить файл: %w", err)
	}
	return nil
}

func nextID(todos []Todo) int {
	max := 0
	for _, t := range todos {
		if t.ID > max {
			max = t.ID
		}
	}
	return max + 1
}

func setDone(s *Storage, id int, done bool) bool {
	for i := range s.Todos {
		if s.Todos[i].ID == id {
			s.Todos[i].Done = done
			return true
		}
	}
	return false
}

func removeTodo(s *Storage, id int) bool {
	for i := range s.Todos {
		if s.Todos[i].ID == id {
			s.Todos = append(s.Todos[:i], s.Todos[i+1:]...)
			return true
		}
	}
	return false
}
