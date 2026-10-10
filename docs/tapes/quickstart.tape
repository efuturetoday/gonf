# Record with: vhs docs/tapes/quickstart.tape
Output docs/tapes/quickstart.gif

Set FontSize 16
Set Width 900
Set Height 450
Set Padding 20

Hide
Type "cd examples/quickstart && clear"
Enter
Show

Type "cat config/app.properties"
Enter
Sleep 2s

Type "go run ."
Enter
Sleep 3s

Type "APP_PORT=9000 go run ."
Enter
Sleep 3s

Type "APP_PORT=9000 go run . app.port"
Enter
Sleep 4s
