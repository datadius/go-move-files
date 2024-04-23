package main

// An example Bubble Tea server. This will put an ssh session into alt screen
// and continually print up to date terminal information.

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	//"github.com/charmbracelet/wish/activeterm"
	"github.com/charmbracelet/wish/bubbletea"
	"github.com/charmbracelet/wish/logging"
	"github.com/charmbracelet/wish/scp"
	"github.com/pkg/sftp"
)

const (
	host = "localhost"
	port = "2022"
)

var (
	files_list []string
)

func main() {
	files_flag := flag.String("f", "", "List of files to move")
	flag.Parse()
	files_list = strings.Split(*files_flag, ",")
	log.Print("Files to move: ", files_list)
	root, _ := filepath.Abs("./testdata/")
	handler := scp.NewFileSystemHandler(root)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	server, err := wish.NewServer(
		wish.WithAddress(net.JoinHostPort(host, port)),
		wish.WithHostKeyPath("./.ssh/id_ed25519"),
		wish.WithPasswordAuth(func(ctx ssh.Context, password string) bool {
			return password == "tiger"
		}),
		wish.WithSubsystem("sftp", sftpSubsystem(root)),
		wish.WithMiddleware(
			//bubbletea.Middleware(teaHandler),
			//activeterm.Middleware(), // Bubble Tea apps usually require a PTY. TODO: Find what PTY is
			scp.Middleware(handler, handler),
			logging.StructuredMiddleware(),
		),
	)
	if err != nil {
		log.Error("Count not start server", "error", err)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	log.Info("Starting SSH server", "host", host, "port", port)
	go func() {
		if err = server.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			log.Error("Could not start server", "error", err)
			done <- nil
		}
	}()

	<-done
	log.Info("Stopping SSH Server")
	defer func() { cancel() }()
	if err := server.Shutdown(ctx); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		log.Error("Count not stop server", "error", err)
	}
}

func sftpSubsystem(root string) ssh.SubsystemHandler {
	return func(s ssh.Session) {
		log.Info("sftp", "root", root)
		fs := &sftpHandler{root}
		srv := sftp.NewRequestServer(s, sftp.Handlers{
			FileList: fs,
			FileGet:  fs,
		})
		if err := srv.Serve(); err == io.EOF {
			if err := srv.Close(); err != nil {
				wish.Fatalln(s, "sftp:", err)
			}
		} else if err != nil {
			wish.Fatalln(s, "sftp:", err)
		}
	}
}

type sftpHandler struct {
	root string
}

var (
	_ sftp.FileLister = &sftpHandler{}
	_ sftp.FileReader = &sftpHandler{}
)

type listerAt []fs.FileInfo

func (l listerAt) ListAt(ls []fs.FileInfo, offset int64) (int, error) {
	if offset >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(ls, l[offset:])
	if n < len(ls) {
		return n, io.EOF
	}
	return n, nil
}

// Fileread implements sftp.FileReader.
func (s *sftpHandler) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	var flags int
	pflags := r.Pflags()
	if pflags.Append {
		flags |= os.O_APPEND
	}
	if pflags.Creat {
		flags |= os.O_CREATE
	}
	if pflags.Excl {
		flags |= os.O_EXCL
	}
	if pflags.Trunc {
		flags |= os.O_TRUNC
	}

	if pflags.Read && pflags.Write {
		flags |= os.O_RDWR
	} else if pflags.Read {
		flags |= os.O_RDONLY
	} else if pflags.Write {
		flags |= os.O_WRONLY
	}

	f, err := os.OpenFile(filepath.Join(s.root, r.Filepath), flags, 0600)
	if err != nil {
		return nil, err
	}

	return f, nil
}

// Filelist implements sftp.FileLister.
func (s *sftpHandler) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	switch r.Method {
	case "List":
		entries, err := os.ReadDir(filepath.Join(s.root, r.Filepath))
		if err != nil {
			return nil, fmt.Errorf("sftp: %w", err)
		}
		infos := make([]fs.FileInfo, len(entries))
		for i, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				return nil, err
			}
			infos[i] = info
		}
		return listerAt(infos), nil
	case "Stat":
		fi, err := os.Stat(filepath.Join(s.root, r.Filepath))
		if err != nil {
			return nil, err
		}
		return listerAt{fi}, nil
	default:
		return nil, sftp.ErrSSHFxOpUnsupported
	}
}

// You can wire any Bubble Tea model up to the middleware with a function that
// handles the incoming ssh.Session. Here we just grab the terminal info and
// pass it to the new model. You can also return tea.ProgramOptions (such as
// tea.WithAltScreen) on a session by session basis.
func teaHandler(s ssh.Session) (tea.Model, []tea.ProgramOption) {
	files, err := os.ReadDir("./testdata/")
	if err != nil {
		log.Fatal("Unable to read dir", "error", err)
	}

	choices := make([]string, 0)
	for _, file := range files {
		for _, file_name := range files_list {
			if file_name == file.Name() {
				choices = append(choices, file.Name())
			}
		}
	}

	// When running a Bubble Tea app over SSH, you shouldn't use the default
	// lipgloss.NewStyle function.
	// That function will use the color profile from the os.Stdin, which is the
	// server, not the client.
	// We provide a MakeRenderer function in the bubbletea middleware package,
	// so you can easily get the correct renderer for the current session, and
	// use it to create the styles.
	// The recommended way to use these styles is to then pass them down to
	// your Bubble Tea model.
	renderer := bubbletea.MakeRenderer(s)
	txtStyle := renderer.NewStyle().Foreground(lipgloss.Color("10"))
	quitStyle := renderer.NewStyle().Foreground(lipgloss.Color("8"))

	m := model{
		session:      s,
		sendingFiles: false,
		sentFiles:    false,
		choices:      choices,
		cursor:       0,
		selected:     make(map[int]string),
		txtStyle:     txtStyle,
		quitStyle:    quitStyle,
	}
	return m, []tea.ProgramOption{tea.WithAltScreen()}
}

// Just a generic tea.Model to demo terminal information of ssh.
type model struct {
	session      ssh.Session
	sendingFiles bool
	sentFiles    bool
	choices      []string
	cursor       int
	selected     map[int]string
	txtStyle     lipgloss.Style
	quitStyle    lipgloss.Style
}

func (m model) Init() tea.Cmd {
	return nil
}

type filesSent string

func SendFiles(session ssh.Session, selectedFiles map[int]string) tea.Cmd {
	return func() tea.Msg {

		for key, file := range selectedFiles {
			log.Info("Sending file", "key", key, "file", file)

		}

		return filesSent("Files sent")

	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}
		case "enter", " ":
			_, ok := m.selected[m.cursor]
			if ok {
				delete(m.selected, m.cursor)
			} else {
				m.selected[m.cursor] = m.choices[m.cursor]
			}
		case "r":
			m.sendingFiles = true
			return m, SendFiles(m.session, m.selected)
		}
	case filesSent:
		m.sendingFiles = false
		m.sentFiles = true
	}
	return m, nil
}

func (m model) View() string {
	if m.sendingFiles {
		return "Sending files..."
	} else if m.sentFiles {
		return "Files sent\nPress q to quit.\n"
	}

	string_view := "These are your options\n"
	// Iterate over our choices
	for i, choice := range m.choices {

		// Is the cursor pointing at this choice?
		cursor := " " // no cursor
		if m.cursor == i {
			cursor = ">" // cursor!
		}

		// Is this choice selected?
		checked := " " // not selected
		if _, ok := m.selected[i]; ok {
			checked = "x" // selected!
		}

		// Render the row
		string_view += fmt.Sprintf("%s [%s] %s\n", cursor, checked, choice)
	}

	// The footer
	string_view += "\nPress q to quit.\nPress r to receive selected files.\n"

	// Send the UI for rendering
	return string_view
}
