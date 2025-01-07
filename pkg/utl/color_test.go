package utl

import "testing"

// ColorTest is a function to test the color package
func TestPrintColors(t *testing.T) {
	t.Log(White("White"))
	t.Log(Red("Red"))
	t.Log(Green("Green"))
	t.Log(Yellow("Yellow"))
	t.Log(Blue("Blue"))
	t.Log(Magenta("Magenta"))
	t.Log(Cyan("Cyan"))
	t.Log(Black("Black"))

	// Bold
	t.Log(BoldWhite("Bold White"))
	t.Log(BoldRed("Bold Red"))
	t.Log(BoldGreen("Bold Green"))
	t.Log(BoldYellow("Bold Yellow"))
	t.Log(BoldBlue("Bold Blue"))
	t.Log(BoldMagenta("Bold Magenta"))
	t.Log(BoldCyan("Bold Cyan"))
	t.Log(BoldBlack("Bold Black"))

	// Faint
	t.Log(FaintWhite("Faint White"))
	t.Log(FaintRed("Faint Red"))
	t.Log(FaintGreen("Faint Green"))
	t.Log(FaintYellow("Faint Yellow"))
	t.Log(FaintBlue("Faint Blue"))
	t.Log(FaintMagenta("Faint Magenta"))
	t.Log(FaintCyan("Faint Cyan"))
	t.Log(FaintBlack("Faint Black"))

	// Italic
	t.Log(ItalicWhite("Italic White"))
	t.Log(ItalicRed("Italic Red"))
	t.Log(ItalicGreen("Italic Green"))
	t.Log(ItalicYellow("Italic Yellow"))
	t.Log(ItalicBlue("Italic Blue"))
	t.Log(ItalicMagenta("Italic Magenta"))
	t.Log(ItalicCyan("Italic Cyan"))
	t.Log(ItalicBlack("Italic Black"))

	// Underline
	t.Log(UnderlineWhite("Underline White"))
	t.Log(UnderlineRed("Underline Red"))
	t.Log(UnderlineGreen("Underline Green"))
	t.Log(UnderlineYellow("Underline Yellow"))
	t.Log(UnderlineBlue("Underline Blue"))
	t.Log(UnderlineMagenta("Underline Magenta"))
	t.Log(UnderlineCyan("Underline Cyan"))
	t.Log(UnderlineBlack("Underline Black"))

	// Blink
	t.Log(BlinkWhite("Blink White"))
	t.Log(BlinkRed("Blink Red"))
	t.Log(BlinkGreen("Blink Green"))
	t.Log(BlinkYellow("Blink Yellow"))
	t.Log(BlinkBlue("Blink Blue"))
	t.Log(BlinkMagenta("Blink Magenta"))
	t.Log(BlinkCyan("Blink Cyan"))
	t.Log(BlinkBlack("Blink Black"))

	// Inverse
	t.Log(InverseWhite("Inverse White"))
	t.Log(InverseRed("Inverse Red"))
	t.Log(InverseGreen("Inverse Green"))
	t.Log(InverseYellow("Inverse Yellow"))
	t.Log(InverseBlue("Inverse Blue"))
	t.Log(InverseMagenta("Inverse Magenta"))
	t.Log(InverseCyan("Inverse Cyan"))
	t.Log(InverseBlack("Inverse Black"))

}
