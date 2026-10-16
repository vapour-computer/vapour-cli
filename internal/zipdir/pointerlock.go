package zipdir

import (
	"bytes"
	"regexp"
)

// PointerLockMarker identifies the injected script so publishing twice never adds it twice.
const PointerLockMarker = `data-vapour="pointer-lock"`

func pointerLockScript(pauseOnLoss bool) string {
	pause := "false"
	if pauseOnLoss {
		pause = "true"
	}
	return `<script ` + PointerLockMarker + `>(function(){if(window.__vapourPointerLock)return;window.__vapourPointerLock=1;var P=Element.prototype,D=Document.prototype,rl=P.requestPointerLock,ex=D.exitPointerLock;var want=false,target=null,overlay=null,ignoreUntil=0,spikeUntil=0,spike=200,pauseOnLoss=` + pause + `;var hasFinePointer=typeof matchMedia==='function'&&matchMedia('(pointer: fine)').matches,sawMouse=false;addEventListener('pointermove',function(e){if(e&&e.pointerType==='mouse')sawMouse=true;},true);addEventListener('mousemove',function(){sawMouse=true;},true);` +
		`function keyEscape(t){var dst=t&&t.isConnected?t:document;try{var i={key:'Escape',code:'Escape',keyCode:27,which:27,bubbles:true,cancelable:true};var kd=new KeyboardEvent('keydown',i),ku=new KeyboardEvent('keyup',i);try{Object.defineProperty(kd,'keyCode',{get:function(){return 27;}});Object.defineProperty(kd,'which',{get:function(){return 27;}});Object.defineProperty(ku,'keyCode',{get:function(){return 27;}});Object.defineProperty(ku,'which',{get:function(){return 27;}});}catch(_){}dst.dispatchEvent(kd);dst.dispatchEvent(ku);}catch(_){}}` +
		`function swallow(e){e.preventDefault();e.stopPropagation();if(e.stopImmediatePropagation)e.stopImmediatePropagation();}` +
		`function ensure(){if(overlay)return overlay;overlay=document.createElement('button');overlay.type='button';overlay.setAttribute('aria-label','Click to resume');overlay.style.cssText='position:fixed;inset:0;z-index:2147483647;display:none;border:0;background:rgba(8,12,20,.55);color:#fff;font:600 18px/1.2 system-ui,sans-serif;cursor:pointer;';overlay.innerHTML='<span style="display:inline-block;padding:10px 14px;border-radius:10px;background:#111827;border:1px solid rgba(255,255,255,.25)">Click to play</span>';overlay.addEventListener('pointerdown',function(e){swallow(e);if(want&&target&&target.isConnected)request(target);},true);overlay.addEventListener('mousedown',swallow,true);overlay.addEventListener('mouseup',swallow,true);overlay.addEventListener('click',swallow,true);document.documentElement.appendChild(overlay);return overlay;}` +
		`function show(msg){if(!hasFinePointer&&!sawMouse)return;var o=ensure();o.firstChild.textContent=msg||'Click to play';o.style.display='block';}` +
		`function hide(){if(overlay)overlay.style.display='none';}` +
		`function request(el){if(!el||!el.isConnected)return;var res;try{res=rl.call(el,{unadjustedMovement:true});}catch(_){try{res=rl.call(el);}catch(__){show('Click to play');return;}}if(res&&typeof res.catch==='function'){res.catch(function(){try{rl.call(el);}catch(_){show('Click to play');}});}setTimeout(function(){if(want&&document.pointerLockElement!==el)show('Click to play');},80);return res;}` +
		`P.requestPointerLock=function(){want=true;target=this;hide();return request(this);};` +
		`D.exitPointerLock=function(){want=false;target=null;hide();return ex.apply(this,arguments);};` +
		`document.addEventListener('pointerlockchange',function(){var locked=target&&document.pointerLockElement===target;if(locked){ignoreUntil=performance.now()+20;spikeUntil=performance.now()+250;hide();return;}if(want&&target&&target.isConnected){show('Click to resume');if(pauseOnLoss)keyEscape(target);}},true);` +
		`addEventListener('mousemove',function(e){if(!target||document.pointerLockElement!==target)return;var now=performance.now();if(now<ignoreUntil){swallow(e);return;}if(now<spikeUntil&&(Math.abs(e.movementX)>spike||Math.abs(e.movementY)>spike))swallow(e);},true);})();</script>`
}

var headClose = regexp.MustCompile(`(?i)</head\s*>`)
var headOpen = regexp.MustCompile(`(?i)<head[^>]*>`)

// InjectPointerLock returns html with the pointer-lock script added to <head>.
// It returns html unchanged if the script is already present.
func InjectPointerLock(html []byte, pauseOnLoss bool) []byte {
	if bytes.Contains(html, []byte(PointerLockMarker)) {
		return html
	}
	script := []byte(pointerLockScript(pauseOnLoss))
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
