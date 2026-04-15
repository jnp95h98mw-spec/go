package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Todo struct {
	ID        int       `json:"id"`
	Text      string    `json:"text"`
	Note      string    `json:"note"`
	Labels    []string  `json:"labels"`
	Project   string    `json:"project"`
	Priority  int       `json:"priority"`
	DueDate   string    `json:"due_date"`
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
			return errors.New("укажите текст задачи: todo add [--project <название>] \"Купить молоко\"")
		}
		project, rest, err := parseProjectArg(args[1:])
		if err != nil {
			return err
		}
		text := strings.TrimSpace(strings.Join(rest, " "))
		if text == "" {
			return errors.New("текст задачи не может быть пустым")
		}
		nextID := nextID(s.Todos)
		s.Todos = append(s.Todos, Todo{ID: nextID, Text: text, Project: project, Priority: 4, CreatedAt: time.Now()})
		if err := save(path, s); err != nil {
			return err
		}
		if project == "" {
			project = "inbox"
		}
		fmt.Printf("Добавлено: [%d] (%s) %s\n", nextID, project, text)

	case "list":
		filterProject := ""
		if len(args) == 2 {
			filterProject = strings.TrimSpace(args[1])
		}
		if len(s.Todos) == 0 {
			fmt.Println("Список задач пуст")
			return nil
		}
		for _, t := range s.Todos {
			if filterProject != "" && normalizeProject(t.Project) != normalizeProject(filterProject) {
				continue
			}
			mark := " "
			if t.Done {
				mark = "x"
			}
			fmt.Printf("[%s] %d. (%s) %s\n", mark, t.ID, projectName(t.Project), t.Text)
		}

	case "projects":
		projects := collectProjects(s.Todos)
		if len(projects) == 0 {
			fmt.Println("Проектов пока нет")
			return nil
		}
		fmt.Println("Проекты:")
		for _, p := range projects {
			fmt.Printf("- %s\n", p)
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
	fmt.Println("  add --project P Добавить задачу в проект P")
	fmt.Println("  list [проект]   Показать задачи (или по проекту)")
	fmt.Println("  projects        Список проектов")
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
  <title>TaskFlow Pro</title>
  <style>
    :root { --bg:#0f1222; --bg2:#171a2b; --card:rgba(255,255,255,.06); --line:rgba(255,255,255,.11); --text:#eef1ff; --muted:#9ea5d1; --accent:#7c5cff; --accent2:#34d1bf; --danger:#ff5e7d; --ok:#59d58f; }
    * { box-sizing: border-box; }
    html, body { height: 100%; }
    body { margin:0; font-family: Inter, system-ui, sans-serif; background:radial-gradient(circle at 20% -10%, #2d1f55 0%, transparent 40%), radial-gradient(circle at 80% 120%, #123d46 0%, transparent 40%), var(--bg); color:var(--text); }
    .layout { display:grid; grid-template-columns:280px 1fr; min-height:100vh; backdrop-filter: blur(4px); }
    .sidebar { background:linear-gradient(180deg, rgba(255,255,255,.09), rgba(255,255,255,.04)); border-right:1px solid var(--line); padding:1.1rem; }
    .brand { font-weight:800; margin-bottom:1rem; letter-spacing:.02em; display:flex; align-items:center; gap:.5rem;}
    .brand .dot{width:10px;height:10px;border-radius:99px;background:linear-gradient(120deg,var(--accent),var(--accent2));box-shadow:0 0 16px var(--accent);}
    .nav a { display:flex; justify-content:space-between; text-decoration:none; color:var(--text); padding:.52rem .6rem; border-radius:10px; margin-bottom:.25rem; transition: .2s ease;}
    .nav a.active, .nav a:hover { background:rgba(255,255,255,.1); transform: translateX(2px); }
    .projects { margin-top:1rem; }
    .projects h3 { font-size:.9rem; color:var(--muted); margin:0 0 .4rem; text-transform:uppercase; letter-spacing:.03em; }
    .main { padding:1.4rem; }
    .panel { background:linear-gradient(180deg, rgba(255,255,255,.08), rgba(255,255,255,.04)); border:1px solid var(--line); border-radius:16px; padding:1.1rem; box-shadow: 0 20px 50px rgba(0,0,0,.25); animation: panelIn .5s ease;}
    h1 { margin:.1rem 0 1rem; font-size:1.3rem; }
    .meta { color:var(--muted); font-size:.85rem; margin-bottom:1rem; }
    .add { display:grid; grid-template-columns:1fr 170px 120px 110px; gap:.5rem; margin-bottom:.7rem; }
    input, select, button { padding:.62rem .7rem; border:1px solid var(--line); border-radius:10px; background:rgba(255,255,255,.05); color:var(--text); transition:.2s ease; }
    input:focus, select:focus { outline:none; border-color:var(--accent); box-shadow:0 0 0 3px rgba(124,92,255,.25); }
    button { cursor:pointer; }
    button:hover { transform: translateY(-1px); }
    button.primary { background:linear-gradient(120deg,var(--accent),#5f9dff); color:#fff; border:none; font-weight:700; }
    button.primary:hover { box-shadow:0 8px 24px rgba(124,92,255,.35); }
    ul { list-style:none; padding:0; margin:0; }
    li.todo { display:grid; grid-template-columns:1fr auto; gap:.6rem; align-items:center; border-top:1px solid var(--line); padding:.8rem 0; animation: itemIn .35s ease both; }
    .left { display:flex; gap:.55rem; align-items:flex-start; }
    .title.done { text-decoration:line-through; color:var(--muted); }
    .sub { font-size:.82rem; color:var(--muted); margin-top:.15rem; }
    .prio { font-weight:600; margin-right:.35rem; }
    .p1{color:#ff7c93}.p2{color:#ffc857}.p3{color:#6ab7ff}.p4{color:#c8cbe5}
    .actions { display:flex; gap:.4rem; }
    .empty { color:var(--muted); padding:.8rem 0; }
    .clear { margin-top:.8rem; }
    .chip { display:inline-flex; align-items:center; gap:.35rem; border:1px solid var(--line); border-radius:999px; padding:.25rem .55rem; font-size:.78rem; color:var(--muted); }
    .pill { font-size:.75rem; font-weight:700; color:#fff; background:linear-gradient(120deg,#2bcf9c,#32d2ff); border-radius:999px; padding:.15rem .45rem; }
    .statrow { display:flex; gap:.5rem; margin-bottom:.75rem; flex-wrap:wrap; }
    .toast { position:fixed; right:1rem; bottom:1rem; background:rgba(35,38,60,.95); border:1px solid var(--line); color:var(--text); padding:.7rem .85rem; border-radius:10px; opacity:0; transform:translateY(10px); transition:.25s ease; pointer-events:none; }
    .toast.show { opacity:1; transform:translateY(0); }
    .done-btn{border-color:rgba(89,213,143,.35);}
    .delete-btn{border-color:rgba(255,94,125,.35);}
    @keyframes panelIn { from { opacity:0; transform:translateY(8px);} to {opacity:1; transform:none;} }
    @keyframes itemIn { from { opacity:0; transform:translateY(6px);} to {opacity:1; transform:none;} }
    @media (max-width: 900px) { .layout { grid-template-columns:1fr; } .sidebar{border-right:none;border-bottom:1px solid var(--line);} .add{grid-template-columns:1fr 1fr;} }
  </style>
</head>
<body>
  <div class="layout">
    <aside class="sidebar">
      <div class="brand"><span class="dot"></span>TaskFlow Pro</div>
      <nav class="nav">
        <a class="{{if eq .View "inbox"}}active{{end}}" href="/?view=inbox"><span>Входящие</span><span>{{.Counts.Inbox}}</span></a>
        <a class="{{if eq .View "today"}}active{{end}}" href="/?view=today"><span>Сегодня</span><span>{{.Counts.Today}}</span></a>
        <a class="{{if eq .View "upcoming"}}active{{end}}" href="/?view=upcoming"><span>Предстоящее</span><span>{{.Counts.Upcoming}}</span></a>
        <a class="{{if eq .View "overdue"}}active{{end}}" href="/?view=overdue"><span>Просроченные</span><span>{{.Counts.Overdue}}</span></a>
        <a class="{{if and (eq .View "inbox") .ShowDone}}active{{end}}" href="/?view=inbox&show_done=1"><span>Выполненные</span><span>{{.Counts.Done}}</span></a>
      </nav>
      <div class="projects">
        <h3>Проекты</h3>
        <nav class="nav">
          {{range .Projects}}
          <a class="{{if and (eq $.View "project") (eq $.ProjectFilter .)}}active{{end}}" href="/?view=project&project={{.}}"><span>{{.}}</span><span>{{index $.ProjectCounts .}}</span></a>
          {{end}}
        </nav>
      </div>
    </aside>
    <main class="main">
      <div class="panel">
        <h1>{{.Title}}</h1>
        <div class="meta">Хранилище: {{.Path}}</div>
        <div class="statrow">
          <span class="chip">Активные: <strong>{{add .Counts.Inbox .Counts.Today .Counts.Upcoming}}</strong></span>
          <span class="chip">Текущий вид: <span class="pill">{{.View}}</span></span>
        </div>
        <form class="add" method="get" action="/" style="margin-bottom:.9rem;grid-template-columns:1fr 150px;">
          <input type="hidden" name="view" value="{{.View}}" />
          <input type="text" name="q" value="{{.Query}}" placeholder="Поиск задач..." />
          <button type="submit">Поиск</button>
        </form>
        <form class="add" method="post" action="/add">
          <input type="text" name="text" placeholder="Что нужно сделать?" required />
          <input type="text" name="project" placeholder="Проект (по умолчанию inbox)" />
          <input type="date" name="due_date" />
          <div style="display:flex;gap:.5rem;">
            <select name="priority">
              <option value="4">P4</option>
              <option value="3">P3</option>
              <option value="2">P2</option>
              <option value="1">P1</option>
            </select>
            <button class="primary" type="submit">Добавить</button>
          </div>
          <input type="text" name="labels" placeholder="Метки через запятую: work,urgent" />
          <input type="text" name="note" placeholder="Описание / комментарий" />
          <input type="hidden" name="back" value="{{.BackURL}}" />
        </form>

        {{if .Todos}}
        <ul>
          {{range .Todos}}
          <li class="todo">
            <div class="left">
              <form method="post" action="/toggle">
                <input type="hidden" name="id" value="{{.ID}}" />
                <input type="hidden" name="back" value="{{$.BackURL}}" />
                <button class="done-btn" type="submit">{{if .Done}}↩{{else}}✓{{end}}</button>
              </form>
              <div>
                <div class="title {{if .Done}}done{{end}}">{{.Text}}</div>
                <div class="sub"><span class="prio p{{.Priority}}">P{{.Priority}}</span>Проект: {{if .Project}}{{.Project}}{{else}}inbox{{end}}{{if .DueDate}} · Срок: {{.DueDate}}{{end}}</div>
                {{if .Labels}}<div class="sub">🏷️ {{range $i, $l := .Labels}}{{if $i}}, {{end}}{{$l}}{{end}}</div>{{end}}
                {{if .Note}}<div class="sub">📝 {{.Note}}</div>{{end}}
              </div>
            </div>
            <div class="actions">
              <form method="post" action="/delete">
                <input type="hidden" name="id" value="{{.ID}}" />
                <input type="hidden" name="back" value="{{$.BackURL}}" />
                <button class="delete-btn" type="submit">Удалить</button>
              </form>
            </div>
          </li>
          {{end}}
        </ul>
        {{else}}
        <div class="empty">Задач в этом разделе пока нет.</div>
        {{end}}

        <form class="clear" method="post" action="/clear">
          <input type="hidden" name="back" value="{{.BackURL}}" />
          <button type="submit">Очистить всё</button>
        </form>
      </div>
    </main>
  </div>
  <div id="toast" class="toast">Сделано ⚡</div>
  <script>
    const toast = document.getElementById('toast');
    document.querySelectorAll('form[action="/add"],form[action="/toggle"],form[action="/delete"],form[action="/clear"]').forEach((f) => {
      f.addEventListener('submit', () => {
        toast.classList.add('show');
        setTimeout(() => toast.classList.remove('show'), 900);
      });
    });
  </script>
</body>
</html>`

func runWeb(path string, s *Storage, port string) error {
	tpl, err := template.New("page").Funcs(template.FuncMap{
		"add": func(v ...int) int {
			sum := 0
			for _, n := range v {
				sum += n
			}
			return sum
		},
	}).Parse(pageTpl)
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
		all := append([]Todo(nil), s.Todos...)
		storeMu.Unlock()
		view := strings.TrimSpace(r.URL.Query().Get("view"))
		if view == "" {
			view = "inbox"
		}
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		showDone := r.URL.Query().Get("show_done") == "1"
		projectFilter := normalizeProject(r.URL.Query().Get("project"))

		counts, projectCounts := computeCounts(all)
		filtered := filterTodos(all, view, projectFilter, showDone, query)
		data := struct {
			Todos         []Todo
			Path          string
			View          string
			Query         string
			ShowDone      bool
			ProjectFilter string
			Projects      []string
			Title         string
			BackURL       string
			Counts        ViewCounts
			ProjectCounts map[string]int
		}{
			Todos:         filtered,
			Path:          path,
			View:          view,
			Query:         query,
			ShowDone:      showDone,
			ProjectFilter: projectFilter,
			Projects:      collectProjects(all),
			Title:         resolveTitle(view, projectFilter),
			BackURL:       currentBackURL(view, projectFilter, showDone, query),
			Counts:        counts,
			ProjectCounts: projectCounts,
		}

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
		project := strings.TrimSpace(r.FormValue("project"))
		dueDate := strings.TrimSpace(r.FormValue("due_date"))
		priority := parsePriority(r.FormValue("priority"))
		note := strings.TrimSpace(r.FormValue("note"))
		labels := parseLabels(r.FormValue("labels"))
		if text == "" {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		dueDate = normalizeDueDate(dueDate)
		storeMu.Lock()
		s.Todos = append(s.Todos, Todo{
			ID:        nextID(s.Todos),
			Text:      text,
			Note:      note,
			Labels:    labels,
			Project:   normalizeProject(project),
			Priority:  priority,
			DueDate:   dueDate,
			CreatedAt: time.Now(),
		})
		_ = save(path, s)
		storeMu.Unlock()
		http.Redirect(w, r, backURL(r), http.StatusSeeOther)
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
		http.Redirect(w, r, backURL(r), http.StatusSeeOther)
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
		http.Redirect(w, r, backURL(r), http.StatusSeeOther)
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
		http.Redirect(w, r, backURL(r), http.StatusSeeOther)
	})

	addr := ":" + port
	fmt.Printf("Веб-версия запущена: http://localhost%s\n", addr)
	return http.ListenAndServe(addr, mux)
}

type ViewCounts struct {
	Inbox    int
	Today    int
	Upcoming int
	Overdue  int
	Done     int
}

func filterTodos(all []Todo, view, project string, showDone bool, query string) []Todo {
	out := make([]Todo, 0, len(all))
	today := todayDate()
	q := strings.ToLower(strings.TrimSpace(query))
	for _, t := range all {
		if !showDone && t.Done {
			continue
		}
		if q != "" && !matchesQuery(t, q) {
			continue
		}
		switch view {
		case "today":
			if t.DueDate == today {
				out = append(out, t)
			}
		case "upcoming":
			if isUpcoming(t.DueDate, today) {
				out = append(out, t)
			}
		case "overdue":
			if isOverdue(t.DueDate, today) {
				out = append(out, t)
			}
		case "project":
			if normalizeProject(t.Project) == project {
				out = append(out, t)
			}
		default: // inbox
			if normalizeProject(t.Project) == "" || normalizeProject(t.Project) == "inbox" {
				out = append(out, t)
			}
		}
	}
	return out
}

func computeCounts(all []Todo) (ViewCounts, map[string]int) {
	today := todayDate()
	counts := ViewCounts{}
	projectCounts := map[string]int{}
	for _, t := range all {
		if t.Done {
			counts.Done++
			continue
		}
		p := projectName(t.Project)
		projectCounts[p]++
		if p == "inbox" {
			counts.Inbox++
		}
		if t.DueDate == today {
			counts.Today++
		}
		if isUpcoming(t.DueDate, today) {
			counts.Upcoming++
		}
		if isOverdue(t.DueDate, today) {
			counts.Overdue++
		}
	}
	return counts, projectCounts
}

func resolveTitle(view, project string) string {
	switch view {
	case "today":
		return "Сегодня"
	case "upcoming":
		return "Предстоящее"
	case "overdue":
		return "Просроченные"
	case "project":
		if project == "" {
			return "Проект"
		}
		return "Проект: " + project
	default:
		return "Входящие"
	}
}

func currentBackURL(view, project string, showDone bool, query string) string {
	params := make([]string, 0, 3)
	if view != "" {
		params = append(params, "view="+url.QueryEscape(view))
	}
	if view == "project" && project != "" {
		params = append(params, "project="+url.QueryEscape(project))
	}
	if showDone {
		params = append(params, "show_done=1")
	}
	if query != "" {
		params = append(params, "q="+url.QueryEscape(query))
	}
	if len(params) == 0 {
		return "/?view=inbox"
	}
	return "/?" + strings.Join(params, "&")
}

func backURL(r *http.Request) string {
	back := strings.TrimSpace(r.FormValue("back"))
	if back == "" || back[0] != '/' {
		return "/?view=inbox"
	}
	return back
}

func todayDate() string {
	return time.Now().Format("2006-01-02")
}

func isUpcoming(dueDate, today string) bool {
	if dueDate == "" || dueDate <= today {
		return false
	}
	return true
}

func isOverdue(dueDate, today string) bool {
	if dueDate == "" {
		return false
	}
	return dueDate < today
}

func matchesQuery(t Todo, q string) bool {
	if strings.Contains(strings.ToLower(t.Text), q) || strings.Contains(strings.ToLower(t.Note), q) {
		return true
	}
	for _, l := range t.Labels {
		if strings.Contains(strings.ToLower(l), q) {
			return true
		}
	}
	return strings.Contains(strings.ToLower(projectName(t.Project)), q)
}

func parseLabels(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	items := strings.Split(raw, ",")
	seen := map[string]struct{}{}
	labels := make([]string, 0, len(items))
	for _, item := range items {
		l := strings.TrimSpace(strings.ToLower(item))
		if l == "" {
			continue
		}
		if _, ok := seen[l]; ok {
			continue
		}
		seen[l] = struct{}{}
		labels = append(labels, l)
	}
	sort.Strings(labels)
	return labels
}

func parsePriority(raw string) int {
	p, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 4
	}
	if p < 1 || p > 4 {
		return 4
	}
	return p
}

func normalizeDueDate(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if _, err := time.Parse("2006-01-02", raw); err != nil {
		return ""
	}
	return raw
}

func parseProjectArg(args []string) (project string, rest []string, err error) {
	if len(args) >= 2 && (args[0] == "--project" || args[0] == "-p") {
		project = strings.TrimSpace(args[1])
		if project == "" {
			return "", nil, errors.New("название проекта не может быть пустым")
		}
		return normalizeProject(project), args[2:], nil
	}
	return "", args, nil
}

func normalizeProject(project string) string {
	return strings.TrimSpace(strings.ToLower(project))
}

func projectName(project string) string {
	if strings.TrimSpace(project) == "" {
		return "inbox"
	}
	return project
}

func collectProjects(todos []Todo) []string {
	seen := map[string]struct{}{}
	for _, t := range todos {
		seen[projectName(t.Project)] = struct{}{}
	}
	projects := make([]string, 0, len(seen))
	for p := range seen {
		projects = append(projects, p)
	}
	sort.Strings(projects)
	return projects
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
	for i := range s.Todos {
		if s.Todos[i].Priority < 1 || s.Todos[i].Priority > 4 {
			s.Todos[i].Priority = 4
		}
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
