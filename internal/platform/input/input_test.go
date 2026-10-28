package input

import (
	"reflect"
	"testing"
	"time"
)

func TestJoyButtons(t *testing.T) {
	var m Mapper
	cases := map[uint8]Button{0: B, 1: A, 2: Y, 3: X, 4: L1, 5: R1, 6: Select, 7: Start, 8: Menu, 13: VolDown, 14: VolUp}
	for code, want := range cases {
		if got := m.JoyButton(code, true); !reflect.DeepEqual(got, []Event{{want, Press}}) {
			t.Errorf("button %d = %v, want %v press", code, got, want)
		}
	}
	if got := m.JoyButton(1, false); !reflect.DeepEqual(got, []Event{{A, Release}}) {
		t.Errorf("release = %v", got)
	}
	if got := m.JoyButton(42, true); got != nil {
		t.Errorf("unknown button = %v", got)
	}
}

func TestHatTransitions(t *testing.T) {
	var m Mapper
	if got := m.JoyHat(1); !reflect.DeepEqual(got, []Event{{Up, Press}}) {
		t.Fatalf("up = %v", got)
	}
	if got := m.JoyHat(1 | 2); !reflect.DeepEqual(got, []Event{{Right, Press}}) {
		t.Fatalf("up+right = %v", got)
	}
	if got := m.JoyHat(0); !reflect.DeepEqual(got, []Event{{Up, Release}, {Right, Release}}) {
		t.Fatalf("centre = %v", got)
	}
}

func TestTriggers(t *testing.T) {
	var m Mapper
	if got := m.JoyAxis(2, 20000); !reflect.DeepEqual(got, []Event{{L2, Press}}) {
		t.Fatalf("L2 pull = %v", got)
	}
	if got := m.JoyAxis(2, 30000); got != nil {
		t.Fatalf("L2 held = %v", got)
	}
	if got := m.JoyAxis(2, 100); !reflect.DeepEqual(got, []Event{{L2, Release}}) {
		t.Fatalf("L2 release = %v", got)
	}
	if got := m.JoyAxis(5, 32767); !reflect.DeepEqual(got, []Event{{R2, Press}}) {
		t.Fatalf("R2 = %v", got)
	}
	if got := m.JoyAxis(0, 32767); got != nil {
		t.Fatalf("stick axis = %v", got)
	}
}

// Knulli's mapping names the buttons by place, Xbox style: its A is the
// bottom face button, the Brick's B.
func TestControllerButtons(t *testing.T) {
	var m Mapper
	cases := map[uint8]Button{0: B, 1: A, 2: Y, 3: X, 4: Select, 5: Menu, 6: Start,
		9: L1, 10: R1, 11: Up, 12: Down, 13: Left, 14: Right}
	for code, want := range cases {
		if got := m.ControllerButton(code, true); !reflect.DeepEqual(got, []Event{{want, Press}}) {
			t.Errorf("button %d = %v, want %v press", code, got, want)
		}
	}
	if got := m.ControllerButton(1, false); !reflect.DeepEqual(got, []Event{{A, Release}}) {
		t.Errorf("release = %v", got)
	}
	if got := m.ControllerButton(7, true); got != nil {
		t.Errorf("left stick = %v", got)
	}
}

// With Knulli's Xbox layout for ports switched on, its mapping names the
// face buttons by the Brick's labels.
func TestLabelledControllerButtons(t *testing.T) {
	m := Mapper{Labelled: true}
	cases := map[uint8]Button{0: A, 1: B, 2: X, 3: Y, 6: Start, 9: L1}
	for code, want := range cases {
		if got := m.ControllerButton(code, true); !reflect.DeepEqual(got, []Event{{want, Press}}) {
			t.Errorf("button %d = %v, want %v press", code, got, want)
		}
	}
}

// The joystick buttons a mapping reads are its "bN" inputs: here the one
// Knulli makes from its Brick Pro pad, Function at 15 included. Hats, axes
// and the GUID and name before them aren't buttons.
func TestMappedButtons(t *testing.T) {
	brickPro := "030000005e0400008e02000014010000,TRIMUI Brick Pro Controller,platform:Linux," +
		"a:b5,b:b6,dpdown:h0.4,guide:b15,leftx:a0,lefty:a1,rightx:a2,righty:a3,lefttrigger:b11," +
		"leftstick:b16,dpleft:h0.8,rightshoulder:b10,leftshoulder:b9,righttrigger:b12,rightstick:b17," +
		"dpright:h0.2,back:b13,start:b14,dpup:h0.1,x:b8,y:b7,"
	want := map[uint8]bool{5: true, 6: true, 7: true, 8: true, 9: true, 10: true, 11: true,
		12: true, 13: true, 14: true, 15: true, 16: true, 17: true}
	if got := MappedButtons(brickPro); !reflect.DeepEqual(got, want) {
		t.Fatalf("MappedButtons() = %v, want %v", got, want)
	}
	if got := MappedButtons("0300,b1 pad:b2,a:+a2,b:-a3,x:a4~,y:b300"); len(got) != 0 {
		t.Fatalf("no buttons: %v", got)
	}
}

func TestControllerTriggers(t *testing.T) {
	var m Mapper
	if got := m.ControllerAxis(4, 32767); !reflect.DeepEqual(got, []Event{{L2, Press}}) {
		t.Fatalf("L2 = %v", got)
	}
	if got := m.ControllerAxis(4, 0); !reflect.DeepEqual(got, []Event{{L2, Release}}) {
		t.Fatalf("L2 release = %v", got)
	}
	if got := m.ControllerAxis(5, 32767); !reflect.DeepEqual(got, []Event{{R2, Press}}) {
		t.Fatalf("R2 = %v", got)
	}
	if got := m.ControllerAxis(0, 32767); got != nil {
		t.Fatalf("stick axis = %v", got)
	}
}

func TestKeys(t *testing.T) {
	var m Mapper
	if got := m.Key(14, true, false); !reflect.DeepEqual(got, []Event{{A, Press}}) {
		t.Errorf("K = %v", got)
	}
	if got := m.Key(102, true, false); !reflect.DeepEqual(got, []Event{{Power, Press}}) {
		t.Errorf("power = %v", got)
	}
	if got := m.Key(82, true, true); got != nil {
		t.Errorf("OS repeat should be ignored, got %v", got)
	}
}

func TestRepeater(t *testing.T) {
	r := NewRepeater()
	t0 := time.Unix(0, 0)
	r.Press(Up, t0)
	if got := r.Due(t0.Add(299 * time.Millisecond)); len(got) != 0 {
		t.Fatalf("repeat before the delay: %v", got)
	}
	if got := r.Due(t0.Add(300 * time.Millisecond)); !reflect.DeepEqual(got, []Event{{Up, Repeat}}) {
		t.Fatalf("first repeat = %v", got)
	}
	if got := r.Due(t0.Add(359 * time.Millisecond)); len(got) != 0 {
		t.Fatalf("repeat before the interval: %v", got)
	}
	if got := r.Due(t0.Add(360 * time.Millisecond)); len(got) != 1 {
		t.Fatalf("second repeat = %v", got)
	}
	r.Release(Up)
	if _, ok := r.Next(); ok {
		t.Fatal("released button still repeating")
	}
}

func TestRepeaterAccelerates(t *testing.T) {
	r := NewRepeater()
	now := time.Unix(0, 0)
	r.Press(Down, now)
	now = now.Add(r.Delay)
	var gap time.Duration
	for i := 0; i < r.FastAfter; i++ {
		if len(r.Due(now)) != 1 {
			t.Fatalf("repeat %d missing", i)
		}
		next, _ := r.Next()
		gap = next.Sub(now)
		now = next
	}
	if gap != r.FastInterval {
		t.Fatalf("interval after %d repeats = %v, want %v", r.FastAfter, gap, r.FastInterval)
	}
}

func TestOnlyDpadRepeats(t *testing.T) {
	r := NewRepeater()
	r.Press(A, time.Unix(0, 0))
	if _, ok := r.Next(); ok {
		t.Fatal("A should not repeat")
	}
}
