# Record from the repository root: vhs docs/tapes/error.tape
# The broken config is written to a temporary directory, so no file in
# the repository is changed.
Output docs/tapes/error.gif

Set Shell bash
Set Theme "Dracula"
Set FontSize 20
Set Width 1300
Set Height 360
Set Padding 24
Set Framerate 15
Set TypingSpeed 40ms

Hide
Type "export PS1='$ ' && mkdir -p /tmp/gonf-err/config && go build -o /tmp/gonf-err/app ./examples/quickstart && cd /tmp/gonf-err"
Enter
Type "printf 'app.port=8000\napp.db.host=db.internal\napp.db.ulr=postgres://${app.db.host}:5432/app\n' > config/app.properties && clear"
Enter
Show

Type "cat config/app.properties"
Enter
Sleep 1.5s

Type "./app"
Enter
Sleep 3s
