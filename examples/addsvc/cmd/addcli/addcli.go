package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"

	lightstep "github.com/lightstep/lightstep-tracer-go"
	stdopentracing "github.com/opentracing/opentracing-go"
	zipkinot "github.com/openzipkin-contrib/zipkin-go-opentracing"
	zipkin "github.com/openzipkin/zipkin-go"
	zipkinhttp "github.com/openzipkin/zipkin-go/reporter/http"
	"sourcegraph.com/sourcegraph/appdash"
	appdashot "sourcegraph.com/sourcegraph/appdash/opentracing"

	"github.com/go-kit/kit/log"

	"github.com/go-kit/kit/examples/addsvc/pkg/addservice"
	"github.com/go-kit/kit/examples/addsvc/pkg/addtransport"
)

func main() {
	// The addcli presumes no service discovery system, and expects users to
	// provide the direct address of an addsvc. This presumption is reflected in
	// the addcli binary and the client packages: the -transport.addr flags
	// and various client constructors both expect host:port strings. For an
	// example service with a client built on top of a service discovery system,
	// see profilesvc.
	fs := flag.NewFlagSet("addcli", flag.ExitOnError)
	var (
		httpAddr       = fs.String("http-addr", "", "HTTP address of addsvc")
		zipkinV2URL    = fs.String("zipkin-url", "", "Enable Zipkin v2 tracing (zipkin-go) via HTTP Reporter URL e.g. http://localhost:9411/api/v2/spans")
		zipkinV1URL    = fs.String("zipkin-v1-url", "", "Enable Zipkin v1 tracing (zipkin-go-opentracing) via a collector URL e.g. http://localhost:9411/api/v1/spans")
		lightstepToken = fs.String("lightstep-token", "", "Enable LightStep tracing via a LightStep access token")
		appdashAddr    = fs.String("appdash-addr", "", "Enable Appdash tracing via an Appdash server host:port")
		method         = fs.String("method", "sum", "sum, concat")
	)
	fs.Usage = usageFor(fs, os.Args[0]+" [flags] <a> <b>")
	fs.Parse(os.Args[1:])
	if len(fs.Args()) != 2 {
		fs.Usage()
		os.Exit(1)
	}

	// This is a demonstration client, which supports multiple tracers.
	// Your clients will probably just use one tracer.
	var otTracer stdopentracing.Tracer
	{
		if *zipkinV1URL != "" && *zipkinV2URL == "" {
			collector, err := zipkinot.NewHTTPCollector(*zipkinV1URL)
			if err != nil {
				fmt.Fprintln(os.Stderr, err.Error())
				os.Exit(1)
			}
			defer collector.Close()
			var (
				debug       = false
				hostPort    = "localhost:0"
				serviceName = "addsvc-cli"
			)
			recorder := zipkinot.NewRecorder(collector, debug, hostPort, serviceName)
			otTracer, err = zipkinot.NewTracer(recorder)
			if err != nil {
				fmt.Fprintln(os.Stderr, err.Error())
				os.Exit(1)
			}
		} else if *lightstepToken != "" {
			otTracer = lightstep.NewTracer(lightstep.Options{
				AccessToken: *lightstepToken,
			})
			defer lightstep.FlushLightStepTracer(otTracer)
		} else if *appdashAddr != "" {
			otTracer = appdashot.NewTracer(appdash.NewRemoteCollector(*appdashAddr))
		} else {
			otTracer = stdopentracing.GlobalTracer() // no-op
		}
	}

	// This is a demonstration of the native Zipkin tracing client. If using
	// Zipkin this is the more idiomatic client over OpenTracing.
	var zipkinTracer *zipkin.Tracer
	{
		var (
			err           error
			hostPort      = "" // if host:port is unknown we can keep this empty
			serviceName   = "addsvc-cli"
			useNoopTracer = (*zipkinV2URL == "")
			reporter      = zipkinhttp.NewReporter(*zipkinV2URL)
		)
		defer reporter.Close()
		zEP, _ := zipkin.NewEndpoint(serviceName, hostPort)
		zipkinTracer, err = zipkin.NewTracer(
			reporter, zipkin.WithLocalEndpoint(zEP), zipkin.WithNoopTracer(useNoopTracer),
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to create zipkin tracer: %s\n", err.Error())
			os.Exit(1)
		}
	}

	// This is a demonstration client, which supports multiple transports.
	// Your clients will probably just define and stick with 1 transport.
	var (
		svc addservice.Service
		err error
	)
	if *httpAddr != "" {
		svc, err = addtransport.NewHTTPClient(*httpAddr, otTracer, zipkinTracer, log.NewNopLogger())
	} else {
		fmt.Fprintf(os.Stderr, "error: no remote address specified\n")
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	switch *method {
	case "sum":
		a, _ := strconv.ParseInt(fs.Args()[0], 10, 64)
		b, _ := strconv.ParseInt(fs.Args()[1], 10, 64)
		v, err := svc.Sum(context.Background(), int(a), int(b))
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stdout, "%d + %d = %d\n", a, b, v)

	case "concat":
		a := fs.Args()[0]
		b := fs.Args()[1]
		v, err := svc.Concat(context.Background(), a, b)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stdout, "%q + %q = %q\n", a, b, v)

	default:
		fmt.Fprintf(os.Stderr, "error: invalid method %q\n", *method)
		os.Exit(1)
	}
}

func usageFor(fs *flag.FlagSet, short string) func() {
	return func() {
		fmt.Fprintf(os.Stderr, "USAGE\n")
		fmt.Fprintf(os.Stderr, "  %s\n", short)
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "FLAGS\n")
		w := tabwriter.NewWriter(os.Stderr, 0, 2, 2, ' ', 0)
		fs.VisitAll(func(f *flag.Flag) {
			fmt.Fprintf(w, "\t-%s %s\t%s\n", f.Name, f.DefValue, f.Usage)
		})
		w.Flush()
		fmt.Fprintf(os.Stderr, "\n")
	}
}
