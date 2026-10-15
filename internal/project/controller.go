package project

import (
	"fmt"
	"strings"
)

var Platforms = []string{"desktop", "native-mobile", "port"}

var ControllerModes = []string{"keyboard-map", "dedicated", "action-map"}

var ControlTypes = []string{"button", "dpad", "joystick"}

type Controller struct {
	Mode     string    `toml:"mode,omitempty" json:"mode,omitempty"`
	Controls []Control `toml:"controls,omitempty" json:"controls,omitempty"`
}

type Control struct {
	ID string `toml:"id,omitempty" json:"id,omitempty"`

	Type string  `toml:"type" json:"type"`
	X    float64 `toml:"x" json:"x"`
	Y    float64 `toml:"y" json:"y"`
	W    float64 `toml:"w" json:"w"`
	H    float64 `toml:"h" json:"h"`

	// Keyboard mode: Key/Up/Down/Left/Right are keyboard keys.
	// Dedicated mode: these are action IDs consumed by a native/mobile runtime.
	Key    string `toml:"key,omitempty" json:"key,omitempty"`
	Action string `toml:"action,omitempty" json:"action,omitempty"`
	Up     string `toml:"up,omitempty" json:"up,omitempty"`
	Down   string `toml:"down,omitempty" json:"down,omitempty"`
	Left   string `toml:"left,omitempty" json:"left,omitempty"`
	Right  string `toml:"right,omitempty" json:"right,omitempty"`

	// Joystick shaping for keyboard mapping (x/y deflection -> PWM-style key duty cycle).
	Deadzone    float64 `toml:"deadzone,omitempty" json:"deadzone,omitempty"`
	Sensitivity float64 `toml:"sensitivity,omitempty" json:"sensitivity,omitempty"`
	PWMHz       int     `toml:"pwm_hz,omitempty" json:"pwm_hz,omitempty"`
	RepeatHz    int     `toml:"repeat_hz,omitempty" json:"repeat_hz,omitempty"`
}

// CanonicalMode returns the mode with the action-map alias resolved to dedicated.
func (c Controller) CanonicalMode() string { return canonicalMode(c.Mode) }

func canonicalMode(mode string) string {
	if mode == "action-map" {
		return "dedicated"
	}
	return mode
}

func validatePlatform(p string) error {
	if strings.TrimSpace(p) == "" {
		return nil
	}
	for _, want := range Platforms {
		if p == want {
			return nil
		}
	}
	return fmt.Errorf("project.platform must be one of %s", strings.Join(Platforms, ", "))
}

func (c Controller) Validate() error {
	if len(c.Controls) == 0 && c.Mode == "" {
		return nil
	}
	if c.Mode == "" {
		return fmt.Errorf("controller.mode is required when controller.controls are set")
	}
	mode := canonicalMode(c.Mode)
	modeOK := false
	for _, v := range ControllerModes {
		if c.Mode == v {
			modeOK = true
			break
		}
	}
	if !modeOK {
		return fmt.Errorf("controller.mode must be one of %s (example: mode = \"keyboard-map\")", strings.Join(ControllerModes, ", "))
	}
	for i, ctl := range c.Controls {
		if err := ctl.validate(mode); err != nil {
			return fmt.Errorf("controller.controls[%d]: %w", i, err)
		}
	}
	return nil
}

func (c Control) validate(mode string) error {
	typeOK := false
	for _, v := range ControlTypes {
		if c.Type == v {
			typeOK = true
			break
		}
	}
	if !typeOK {
		return fmt.Errorf("type must be one of %s (example: type = \"joystick\")", strings.Join(ControlTypes, ", "))
	}
	if c.X < 0 || c.X > 1 || c.Y < 0 || c.Y > 1 {
		return fmt.Errorf("x and y must be between 0 and 1 (example: x = 0.82, y = 0.70)")
	}
	if c.W <= 0 || c.W > 1 || c.H <= 0 || c.H > 1 {
		return fmt.Errorf("w and h must be > 0 and <= 1 (example: w = 0.15, h = 0.15)")
	}

	require := func(v, name string) error {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("%s is required for %s", name, c.Type)
		}
		return nil
	}

	switch c.Type {
	case "button":
		if mode == "dedicated" {
			if err := require(c.Action, "action"); err != nil {
				return err
			}
		} else {
			if err := require(c.Key, "key"); err != nil {
				return err
			}
		}
	case "dpad", "joystick":
		for _, f := range []struct {
			v, name string
		}{{c.Up, "up"}, {c.Down, "down"}, {c.Left, "left"}, {c.Right, "right"}} {
			if err := require(f.v, f.name); err != nil {
				return err
			}
		}
	}

	if c.Type == "joystick" {
		if c.Deadzone < 0 || c.Deadzone >= 1 {
			return fmt.Errorf("deadzone must be >= 0 and < 1 (recommended: 0.10 to 0.25)")
		}
		if c.Sensitivity != 0 && c.Sensitivity <= 0 {
			return fmt.Errorf("sensitivity must be > 0 (recommended: 0.8 to 1.6)")
		}
		repeatHz, err := c.repeatHz()
		if err != nil {
			return err
		}
		if repeatHz != 0 && (repeatHz < 1 || repeatHz > 240) {
			return fmt.Errorf("repeat_hz (or pwm_hz) must be between 1 and 240 (recommended: 30 to 90)")
		}
	}

	return nil
}

func (c Control) repeatHz() (int, error) {
	if c.PWMHz != 0 && c.RepeatHz != 0 && c.PWMHz != c.RepeatHz {
		return 0, fmt.Errorf("repeat_hz (%d) and pwm_hz (%d) disagree; keep one field or set both to the same value", c.RepeatHz, c.PWMHz)
	}
	if c.RepeatHz != 0 {
		return c.RepeatHz, nil
	}
	return c.PWMHz, nil
}
