# Record with: vhs docs/tapes/error.tape
# Writes a broken config to a temporary directory, so no file in the
# repository is changed.
Output docs/tapes/error.gif

Set FontSize 16
Set Width 900
Set Height 400
Set Padding 20

Hide
Type "go build -o /tmp/gonf-quickstart ./examples/quickstart && mkdir -p /tmp/gonf-error/config && cd /tmp/gonf-error"
Enter
Type "printf 'app.port=8000\napp.db.host=db.internal\napp.db.ulr=postgres://${app.db.host}:5432/app\n' > config/app.properties && clear"
Enter
Show

Type "cat config/app.properties"
Enter
Sleep 2s

Type "/tmp/gonf-quickstart"
Enter
Sleep 4s
