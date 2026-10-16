package cmd

import (
	"fmt"
	"html"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vapour-computer/vapour-cli/internal/project"
)

func newController() *cobra.Command {
	root := &cobra.Command{
		Use:   "controller",
		Short: "Initialize and validate mobile controller mappings in vapour.toml",
	}

	var dir, mode string
	var force bool
	var preset string
	var previewHTML bool
	var previewOut string
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Write a starter [controller] layout in vapour.toml",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := project.Load(dir)
			if err != nil {
				return err
			}
			if mode == "" {
				if m.Project.Platform == "native-mobile" {
					mode = "dedicated"
				} else {
					mode = "keyboard-map"
				}
			}
			mode = canonicalMode(mode)
			if !contains([]string{"keyboard-map", "dedicated"}, mode) {
				return fmt.Errorf("--mode must be one of %s", strings.Join(project.ControllerModes, ", "))
			}
			if preset != "" && !contains([]string{"default", "shooter", "dpad-only"}, preset) {
				return fmt.Errorf("--preset must be one of default, shooter, dpad-only")
			}
			if len(m.Controller.Controls) > 0 && !force {
				return fmt.Errorf("controller.controls already exists; pass --force to replace")
			}
			if len(m.Controller.Controls) > 0 && force {
				backup, err := backupManifest(dir)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Backed up existing %s to %s\n", project.File, backup)
			}

			m.Controller.Mode = mode
			m.Controller.Controls = defaultControls(mode, preset)
			if err := m.Validate(); err != nil {
				return err
			}
			if err := m.Save(dir); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Controller config written to %s/%s\n", dir, project.File)
			fmt.Fprintln(cmd.OutOrStdout(), "Next: run `vapour controller preview` and `vapour controller check`.")
			return nil
		},
	}

	check := &cobra.Command{
		Use:   "check",
		Short: "Validate [controller] in vapour.toml",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := project.Load(dir)
			if err != nil {
				return err
			}
			if err := m.Validate(); err != nil {
				return err
			}
			if len(m.Controller.Controls) == 0 && strings.TrimSpace(m.Controller.Mode) == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "No [controller] configured.")
				fmt.Fprintln(cmd.OutOrStdout(), "Try: vapour controller init")
				return nil
			}
			mode := canonicalMode(m.Controller.Mode)
			fmt.Fprintf(cmd.OutOrStdout(), "Controller mode: %s\n", mode)
			for _, w := range compatibilityWarnings(m.Project.Platform, mode) {
				fmt.Fprintf(cmd.OutOrStdout(), "warning: %s\n", w)
			}
			for i, c := range m.Controller.Controls {
				id := c.ID
				if id == "" {
					id = fmt.Sprintf("control-%d", i+1)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  %-12s %-8s at (%.2f, %.2f) size (%.2f, %.2f)\n", id, c.Type, c.X, c.Y, c.W, c.H)
				if c.Type == "joystick" {
					hz := c.RepeatHz
					if hz == 0 {
						hz = c.PWMHz
					}
					if hz == 0 {
						hz = 30
					}
					dz := c.Deadzone
					if dz == 0 {
						dz = 0.15
					}
					sens := c.Sensitivity
					if sens == 0 {
						sens = 1.0
					}
					fmt.Fprintf(cmd.OutOrStdout(), "    joystick tuning: deadzone=%.2f sensitivity=%.2f repeat_hz=%d\n", dz, sens, hz)
				}
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Controller config looks good.")
			fmt.Fprintln(cmd.OutOrStdout(), "Tip: run `vapour controller preview` to see the layout.")
			return nil
		},
	}

	preview := &cobra.Command{
		Use:   "preview",
		Short: "Render an ASCII preview or write an HTML layout preview",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := project.Load(dir)
			if err != nil {
				return err
			}
			if len(m.Controller.Controls) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No controller controls found. Run `vapour controller init` first.")
				return nil
			}
			if err := m.Controller.Validate(); err != nil {
				return fmt.Errorf("preview needs a valid [controller]: %w", err)
			}
			if previewHTML {
				htmlText := renderHTMLPreview(m.Controller.Controls, canonicalMode(m.Controller.Mode), m.Project.Platform)
				path := previewOut
				if strings.TrimSpace(path) == "" {
					path = "controller-preview.html"
				}
				if !filepath.IsAbs(path) {
					path = filepath.Join(dir, path)
				}
				if err := os.WriteFile(path, []byte(htmlText), 0o644); err != nil {
					return err
				}
				abs, err := filepath.Abs(path)
				if err != nil {
					abs = path
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Wrote HTML preview to %s\n", abs)
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), renderPreview(m.Controller.Controls, 72, 20))
			for i, c := range m.Controller.Controls {
				id := c.ID
				if id == "" {
					id = fmt.Sprintf("control-%d", i+1)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%d. %s (%s) %s\n", i+1, id, c.Type, mappingSummary(c))
			}
			return nil
		},
	}
	for _, c := range []*cobra.Command{initCmd, check, preview} {
		c.Flags().StringVar(&dir, "dir", ".", "game folder containing vapour.toml")
		root.AddCommand(c)
	}
	initCmd.Flags().StringVar(&mode, "mode", "", "keyboard-map | dedicated | action-map")
	initCmd.Flags().StringVar(&preset, "preset", "", "default | shooter | dpad-only")
	initCmd.Flags().BoolVar(&force, "force", false, "replace existing controller.controls")
	preview.Flags().BoolVar(&previewHTML, "html", false, "write an HTML layout preview instead of ASCII")
	preview.Flags().StringVar(&previewOut, "out", "controller-preview.html", "output file for --html (relative to --dir if not absolute)")

	return root
}

func defaultControls(mode, preset string) []project.Control {
	if preset == "dpad-only" {
		if mode == "dedicated" {
			return []project.Control{
				{ID: "dpad", Type: "dpad", X: 0.20, Y: 0.78, W: 0.28, H: 0.28, Up: "move_up", Down: "move_down", Left: "move_left", Right: "move_right"},
				{ID: "a", Type: "button", X: 0.82, Y: 0.75, W: 0.16, H: 0.16, Action: "action_primary"},
			}
		}
		return []project.Control{
			{ID: "dpad", Type: "dpad", X: 0.20, Y: 0.78, W: 0.28, H: 0.28, Up: "ArrowUp", Down: "ArrowDown", Left: "ArrowLeft", Right: "ArrowRight"},
			{ID: "a", Type: "button", X: 0.82, Y: 0.75, W: 0.16, H: 0.16, Key: "Space"},
		}
	}
	if mode == "dedicated" {
		if preset == "shooter" {
			return []project.Control{
				{ID: "move", Type: "joystick", X: 0.23, Y: 0.77, W: 0.30, H: 0.30, Up: "move_up", Down: "move_down", Left: "move_left", Right: "move_right", Deadzone: 0.18, Sensitivity: 1.0, RepeatHz: 60},
				{ID: "aim", Type: "joystick", X: 0.77, Y: 0.77, W: 0.30, H: 0.30, Up: "aim_up", Down: "aim_down", Left: "aim_left", Right: "aim_right", Deadzone: 0.12, Sensitivity: 1.25, RepeatHz: 90},
				{ID: "fire", Type: "button", X: 0.84, Y: 0.56, W: 0.13, H: 0.13, Action: "fire"},
			}
		}
		return []project.Control{
			{ID: "dpad", Type: "dpad", X: 0.18, Y: 0.76, W: 0.25, H: 0.25, Up: "move_up", Down: "move_down", Left: "move_left", Right: "move_right"},
			{ID: "left-stick", Type: "joystick", X: 0.35, Y: 0.76, W: 0.28, H: 0.28, Up: "move_up", Down: "move_down", Left: "move_left", Right: "move_right", Deadzone: 0.15, Sensitivity: 1.0, RepeatHz: 30},
			{ID: "a", Type: "button", X: 0.82, Y: 0.70, W: 0.15, H: 0.15, Action: "action_primary"},
			{ID: "b", Type: "button", X: 0.70, Y: 0.82, W: 0.15, H: 0.15, Action: "action_secondary"},
		}
	}
	if preset == "shooter" {
		return []project.Control{
			{ID: "move", Type: "joystick", X: 0.23, Y: 0.77, W: 0.30, H: 0.30, Up: "w", Down: "s", Left: "a", Right: "d", Deadzone: 0.18, Sensitivity: 1.0, RepeatHz: 60},
			{ID: "aim", Type: "joystick", X: 0.77, Y: 0.77, W: 0.30, H: 0.30, Up: "ArrowUp", Down: "ArrowDown", Left: "ArrowLeft", Right: "ArrowRight", Deadzone: 0.12, Sensitivity: 1.25, RepeatHz: 90},
			{ID: "fire", Type: "button", X: 0.84, Y: 0.56, W: 0.13, H: 0.13, Key: "Space"},
		}
	}
	return []project.Control{
		{ID: "dpad", Type: "dpad", X: 0.18, Y: 0.76, W: 0.25, H: 0.25, Up: "ArrowUp", Down: "ArrowDown", Left: "ArrowLeft", Right: "ArrowRight"},
		{ID: "left-stick", Type: "joystick", X: 0.35, Y: 0.76, W: 0.28, H: 0.28, Up: "w", Down: "s", Left: "a", Right: "d", Deadzone: 0.15, Sensitivity: 1.0, RepeatHz: 30},
		{ID: "a", Type: "button", X: 0.82, Y: 0.70, W: 0.15, H: 0.15, Key: "Space"},
		{ID: "b", Type: "button", X: 0.70, Y: 0.82, W: 0.15, H: 0.15, Key: "Shift"},
	}
}

func canonicalMode(mode string) string {
	if mode == "action-map" {
		return "dedicated"
	}
	return mode
}

func backupManifest(dir string) (string, error) {
	src := filepath.Join(dir, project.File)
	b, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s.bak-%s", project.File, time.Now().Format("20060102-150405"))
	dst := filepath.Join(dir, name)
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		return "", err
	}
	return dst, nil
}

func compatibilityWarnings(platform, mode string) []string {
	var out []string
	if platform == "native-mobile" && mode == "keyboard-map" {
		out = append(out, "project.platform is native-mobile but controller.mode is keyboard-map. Consider mode = \"dedicated\" (alias: action-map).")
	}
	if platform == "desktop" && mode == "dedicated" {
		out = append(out, "project.platform is desktop but controller.mode is dedicated. Use keyboard-map if you want key events.")
	}
	if platform == "port" && mode == "keyboard-map" {
		out = append(out, "project.platform is port; dedicated/action-map often gives better native control mapping.")
	}
	return out
}

// renderPreview draws the layout on a landscape screen. Terminal cells are about twice as tall as
// wide, so a 72x20 grid is close to 16:9. Each control gets a box styled by type, with its id inside.
func renderPreview(controls []project.Control, width, height int) string {
	if width < 40 {
		width = 40
	}
	if height < 12 {
		height = 12
	}
	grid := make([][]rune, height)
	for y := range grid {
		row := make([]rune, width)
		for x := range row {
			row[x] = ' '
		}
		grid[y] = row
	}
	put := func(x, y int, r rune) {
		if x >= 0 && x < width && y >= 0 && y < height {
			grid[y][x] = r
		}
	}

	// Screen frame.
	for x := 0; x < width; x++ {
		put(x, 0, '─')
		put(x, height-1, '─')
	}
	for y := 0; y < height; y++ {
		put(0, y, '│')
		put(width-1, y, '│')
	}
	put(0, 0, '╭')
	put(width-1, 0, '╮')
	put(0, height-1, '╰')
	put(width-1, height-1, '╯')

	for i, c := range controls {
		left := clamp(int(math.Round((c.X-c.W/2)*float64(width-1))), 1, width-2)
		right := clamp(int(math.Round((c.X+c.W/2)*float64(width-1))), 1, width-2)
		top := clamp(int(math.Round((c.Y-c.H/2)*float64(height-1))), 1, height-2)
		bottom := clamp(int(math.Round((c.Y+c.H/2)*float64(height-1))), 1, height-2)
		// Keep tiny controls visible: at least 5x3 cells.
		for right-left < 4 && (left > 1 || right < width-2) {
			if left > 1 {
				left--
			}
			if right-left < 4 && right < width-2 {
				right++
			}
		}
		for bottom-top < 2 && (top > 1 || bottom < height-2) {
			if top > 1 {
				top--
			}
			if bottom-top < 2 && bottom < height-2 {
				bottom++
			}
		}

		h, v, tl, tr, bl, br := '─', '│', '┌', '┐', '└', '┘'
		fill := ' '
		switch c.Type {
		case "joystick":
			tl, tr, bl, br = '╭', '╮', '╰', '╯'
			fill = '·'
		case "dpad":
			h, v = '═', '║'
			tl, tr, bl, br = '╔', '╗', '╚', '╝'
		}
		for y := top; y <= bottom; y++ {
			for x := left; x <= right; x++ {
				put(x, y, fill)
			}
		}
		for x := left; x <= right; x++ {
			put(x, top, h)
			put(x, bottom, h)
		}
		for y := top; y <= bottom; y++ {
			put(left, y, v)
			put(right, y, v)
		}
		put(left, top, tl)
		put(right, top, tr)
		put(left, bottom, bl)
		put(right, bottom, br)

		if c.Type == "dpad" || c.Type == "joystick" {
			cx, cy := (left+right)/2, (top+bottom)/2
			if c.Type == "dpad" {
				put(cx, cy, '┼')
				put(cx, top+1, '▲')
				put(cx, bottom-1, '▼')
				put(left+1, cy, '◀')
				put(right-1, cy, '▶')
			} else {
				put(cx, cy, '●')
			}
		}

		// Label on the row just under the top border (or the middle row for buttons).
		label := controlLabel(c, i)
		inner := right - left - 1
		if inner < 1 {
			continue
		}
		r := []rune(label)
		if len(r) > inner {
			r = append(r[:inner-1], '…')
		}
		ly := (top + bottom) / 2
		if c.Type != "button" {
			ly = top + 1
			if bottom-top < 3 {
				ly = top
			}
		}
		lx := left + 1 + (inner-len(r))/2
		for j, ch := range r {
			put(lx+j, ly, ch)
		}
	}

	var sb strings.Builder
	sb.WriteString("Preview (D=dpad, J=joystick, B=button)\n")
	for _, row := range grid {
		sb.WriteString(strings.TrimRight(string(row), " "))
		sb.WriteByte('\n')
	}
	return sb.String()
}

func controlLabel(c project.Control, i int) string {
	if c.ID != "" {
		return c.ID
	}
	return fmt.Sprintf("control-%d", i+1)
}

func renderHTMLPreview(controls []project.Control, mode, platform string) string {
	var body strings.Builder
	body.WriteString("<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">")
	body.WriteString("<title>Vapour Controller Preview</title><style>")
	body.WriteString("body{margin:0;padding:24px;font:14px/1.4 system-ui,Segoe UI,Roboto,sans-serif;background:#0c111b;color:#e8eef9}")
	body.WriteString(".wrap{display:grid;gap:16px;max-width:980px;margin:0 auto}")
	body.WriteString(".meta{display:flex;gap:10px;flex-wrap:wrap}.pill{background:#1a2233;border:1px solid #2d3b57;padding:6px 10px;border-radius:999px}")
	body.WriteString(".phone{position:relative;aspect-ratio:16/9;max-width:760px;width:100%;border-radius:28px;border:2px solid #2d3b57;background:linear-gradient(180deg,#0b1322,#0d1528);overflow:hidden}")
	body.WriteString(".grid{position:absolute;inset:0;background:radial-gradient(circle at center,rgba(255,255,255,.04) 1px,transparent 1px);background-size:24px 24px}")
	body.WriteString(".control{position:absolute;transform:translate(-50%,-50%);border-radius:999px;display:grid;place-items:center;border:2px solid rgba(255,255,255,.26);color:#fff;font-weight:700;letter-spacing:.04em;text-transform:uppercase}")
	body.WriteString(".control--button{background:radial-gradient(circle at 30% 30%,#5f9bff,#2f4fa8)}")
	body.WriteString(".control--dpad{background:linear-gradient(180deg,#4b5d7f,#2f3d57)}")
	body.WriteString(".control--joystick{background:linear-gradient(180deg,#5c6473,#3a4455)}")
	body.WriteString(".control__thumb{width:42%;height:42%;border-radius:999px;background:rgba(255,255,255,.45)}")
	body.WriteString(".label{position:absolute;left:50%;top:100%;transform:translate(-50%,8px);font-size:11px;background:rgba(0,0,0,.45);padding:2px 6px;border-radius:6px;white-space:nowrap}")
	body.WriteString(".list{display:grid;gap:8px}.item{background:#121a2a;border:1px solid #28344d;border-radius:10px;padding:10px 12px}")
	body.WriteString(".item b{font-size:13px}.mono{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;color:#b4c5e8}")
	body.WriteString("</style></head><body><main class=\"wrap\">")
	body.WriteString("<h1>Vapour Controller Preview</h1><div class=\"meta\">")
	body.WriteString("<span class=\"pill\">mode: " + html.EscapeString(mode) + "</span>")
	body.WriteString("<span class=\"pill\">platform: " + html.EscapeString(platform) + "</span>")
	body.WriteString("<span class=\"pill\">controls: " + fmt.Sprintf("%d", len(controls)) + "</span></div>")
	body.WriteString("<section class=\"phone\"><div class=\"grid\"></div>")

	for i, c := range controls {
		id := c.ID
		if id == "" {
			id = fmt.Sprintf("control-%d", i+1)
		}
		style := fmt.Sprintf("left:%.2f%%;top:%.2f%%;width:%.2f%%;height:%.2f%%;", c.X*100, c.Y*100, c.W*100, c.H*100)
		body.WriteString("<div class=\"control control--" + html.EscapeString(c.Type) + "\" style=\"" + style + "\">")
		switch c.Type {
		case "dpad":
			body.WriteString("<span>＋</span>")
		case "joystick":
			body.WriteString("<div class=\"control__thumb\"></div>")
		default:
			body.WriteString("<span>●</span>")
		}
		body.WriteString("<span class=\"label\">" + html.EscapeString(id) + "</span></div>")
	}

	body.WriteString("</section><section class=\"list\">")
	for i, c := range controls {
		id := c.ID
		if id == "" {
			id = fmt.Sprintf("control-%d", i+1)
		}
		body.WriteString("<div class=\"item\"><b>" + html.EscapeString(id) + "</b> · " + html.EscapeString(c.Type) + "<br><span class=\"mono\">")
		body.WriteString(html.EscapeString(mappingSummary(c)))
		body.WriteString("</span></div>")
	}
	body.WriteString("</section></main></body></html>")
	return body.String()
}

func mappingSummary(c project.Control) string {
	switch c.Type {
	case "button":
		if strings.TrimSpace(c.Action) != "" {
			return "action=" + c.Action
		}
		return "key=" + c.Key
	case "dpad", "joystick":
		hz := c.RepeatHz
		if hz == 0 {
			hz = c.PWMHz
		}
		if c.Type == "joystick" {
			return fmt.Sprintf("up=%s down=%s left=%s right=%s deadzone=%.2f sensitivity=%.2f repeat_hz=%d", c.Up, c.Down, c.Left, c.Right, c.Deadzone, c.Sensitivity, hz)
		}
		return fmt.Sprintf("up=%s down=%s left=%s right=%s", c.Up, c.Down, c.Left, c.Right)
	default:
		return ""
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func contains(vs []string, want string) bool {
	for _, v := range vs {
		if v == want {
			return true
		}
	}
	return false
}
