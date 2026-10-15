package zipdir

import (
	"bytes"
	"regexp"
)

// PointerLockMarker identifies the injected script so publishing twice never adds it twice.
const PointerLockMarker = `data-vapour="pointer-lock"`

// pointerLockScript makes mouse-look work for games that capture the mouse at startup.
//
// Browsers only grant pointer lock after a user click, so a request made while the game loads is
// rejected and the cursor stays visible. The script remembers that the game asked for the lock and
// repeats the request on the next click. It forgets the request when the game releases the lock,
// so menus keep a normal cursor. It wraps the standard browser API, so it works for any engine.
const pointerLockScript = `<script ` + PointerLockMarker + `>(function(){var want=false,P=Element.prototype,rl=P.requestPointerLock,ex=Document.prototype.exitPointerLock;` +
	`function quiet(r){if(r&&r.catch)r.catch(function(){});return r;}` +
	`P.requestPointerLock=function(){want=true;return quiet(rl.apply(this,arguments));};` +
	`Document.prototype.exitPointerLock=function(){want=false;return ex.apply(this,arguments);};` +
	`addEventListener('pointerdown',function(){if(want&&!document.pointerLockElement){var c=document.querySelector('canvas');if(c)quiet(rl.call(c));}},true);})();</script>`

var headClose = regexp.MustCompile(`(?i)</head\s*>`)
var headOpen = regexp.MustCompile(`(?i)<head[^>]*>`)

// InjectPointerLock returns html with the pointer-lock script added to <head>.
// It returns html unchanged if the script is already present.
func InjectPointerLock(html []byte) []byte {
	if bytes.Contains(html, []byte(PointerLockMarker)) {
		return html
	}
	script := []byte(pointerLockScript)
	// Put it first in <head> so the wrapper is installed before any game script runs.
	if loc := headOpen.FindIndex(html); loc != nil {
		return splice(html, loc[1], script)
	}
	if loc := headClose.FindIndex(html); loc != nil {
		return splice(html, loc[0], script)
	}
	return append(append([]byte{}, script...), html...)
}

func splice(src []byte, at int, ins []byte) []byte {
	out := make([]byte, 0, len(src)+len(ins))
	out = append(out, src[:at]...)
	out = append(out, ins...)
	return append(out, src[at:]...)
}
