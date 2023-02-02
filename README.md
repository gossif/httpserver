# HttpServer

Default Go http server with middleware for logging and the option to verbose the request and response. 

## Usage

```
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "pong")
	})
	handler := cors.Default().Handler(mux)
	server := httpserver.NewHttpServer(
		httpserver.WithPort("9082"),
		httpserver.WithHandlers(handler),
		httpserver.WithVerbose(false),
	)
	server.Start(context.Background())
```
