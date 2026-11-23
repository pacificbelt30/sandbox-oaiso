package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const minTerminalWidth, minTerminalHeight = 24, 10

// fitDesktopActions keeps button-only forms visible when they wrap, and
// reserves the remaining height for the page's scrollable content.
func fitDesktopActions(body *tview.Flex, form *tview.Form, hint *tview.TextView, compact ...string) {
	labels := make([]string, form.GetButtonCount())
	for i := range labels {
		labels[i] = form.GetButton(i).GetLabel()
	}
	body.SetDrawFunc(func(_ tcell.Screen, x, y, width, height int) (int, int, int, int) {
		available := max(width-2, 1)
		rows, used := 1, 0
		for i, label := range labels {
			if width < 48 && i < len(compact) && compact[i] != "" {
				label = compact[i]
			}
			form.GetButton(i).SetLabel(label)
			labelWidth := tview.TaggedStringWidth(label)
			if used != 0 && available-used < labelWidth {
				rows++
				used = 0
			}
			used += min(labelWidth+4, available-used) + 1
		}
		form.SetHorizontal(rows > 1)
		body.ResizeItem(form, 2*rows+1, 0)
		if hint != nil {
			lines := 0
			if height >= 14 {
				lines = min(2, len(tview.WordWrap(hint.GetText(true), width)))
			}
			body.ResizeItem(hint, lines, 0)
		}
		return x, y, width, height
	})
}

func drawTerminalSizeNotice(screen tcell.Screen) bool {
	width, height := screen.Size()
	if width >= minTerminalWidth && height >= minTerminalHeight {
		return false
	}
	screen.HideCursor()
	message := "Resize to 24×10."
	if width < 16 {
		message = "Resize"
	}
	if width >= 6 && height > 0 {
		tview.Print(screen, message, 0, height/2, width, tview.AlignCenter, tcell.ColorYellow)
	}
	return true
}

// A very small terminal cannot show a review and its actions together.
// Keep the selected screen and block confirmation until it is readable again.
func guardTerminalSize(app *tview.Application, root tview.Primitive) {
	tooSmall := func() bool {
		_, _, width, height := root.GetRect()
		return width < minTerminalWidth || height < minTerminalHeight
	}
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if tooSmall() && event.Key() != tcell.KeyCtrlC {
			return nil
		}
		return event
	})
	app.SetMouseCapture(func(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
		if tooSmall() {
			return nil, action
		}
		return event, action
	})
}
