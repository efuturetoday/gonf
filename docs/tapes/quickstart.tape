# Record from the repository root: vhs docs/tapes/quickstart.tape
Output docs/tapes/quickstart.gif

Set Shell bash
Set Theme "Dracula"
Set FontSize 20
Set Width 1100
Set Height 420
Set Padding 24
Set Framerate 15
Set TypingSpeed 40ms

Hide
Type "export PS1='$ ' && cd examples/quickstart && go build -o /dev/null . && clear"
Enter
Show

Type "cat config/app.properties"
Enter
Sleep 1.5s

Type "go run ."
Enter
Sleep 2s

Type "APP_PORT=9000 go run ."
Enter
Sleep 3s
