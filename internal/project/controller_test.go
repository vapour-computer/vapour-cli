package project

import "testing"

func TestControllerModeAliasAndRepeatHzAlias(t *testing.T) {
	c := Controller{
		Mode: "action-map",
		Controls: []Control{{
			Type:        "joystick",
			X:           0.4,
			Y:           0.8,
			W:           0.2,
			H:           0.2,
			Up:          "u",
			Down:        "d",
			Left:        "l",
			Right:       "r",
			Sensitivity: 1.0,
			Deadzone:    0.2,
			RepeatHz:    60,
		}},
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("action-map alias should validate: %v", err)
	}

	c.Controls[0].PWMHz = 30
	if err := c.Validate(); err == nil {
		t.Fatal("expected repeat_hz/pwm_hz conflict")
	}
	c.Controls[0].PWMHz = 60
	if err := c.Validate(); err != nil {
		t.Fatalf("matching repeat_hz/pwm_hz should validate: %v", err)
	}
}
