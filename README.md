# tail a file and send to a remote logging server

```bash
Usage of ./teller:
  -file string
    	File to tail (default "log.txt")
  -server string
    	QUIC server address (default "remote-server:5140")

# run the command
./teller -file /var/log/messages
```

## see remote server for more

https://github.com/rexlx/rider
