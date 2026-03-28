package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	sdk "github.com/cordum/cordum/sdk/client"
)

func runPoolCmd(args []string) {
	if len(args) < 1 {
		poolUsage()
		os.Exit(1)
	}
	switch args[0] {
	case "list":
		poolList(args[1:])
	case "get":
		poolGet(args[1:])
	case "create":
		poolCreate(args[1:])
	case "update":
		poolUpdate(args[1:])
	case "delete":
		poolDelete(args[1:])
	case "drain":
		poolDrain(args[1:])
	case "topic":
		poolTopic(args[1:])
	default:
		poolUsage()
		os.Exit(1)
	}
}

func poolUsage() {
	fmt.Fprintln(os.Stderr, `Usage: cordumctl pool <command>

Commands:
  list                          List all pools
  get <name>                    Get pool details
  create <name>                 Create a new pool
  update <name>                 Update pool configuration
  delete <name>                 Delete a pool
  drain <name>                  Start draining a pool
  topic add <pool> <topic>      Add topic mapping
  topic remove <pool> <topic>   Remove topic mapping`)
}

func poolList(args []string) {
	fs := newFlagSet("pool list")
	fs.ParseArgs(args)
	client := newClientFromFlags(fs)

	pools, err := client.ListPools(context.Background())
	check(err)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tWORKERS\tACTIVE JOBS\tCAPACITY\tUTILIZATION")
	for _, p := range pools {
		util := fmt.Sprintf("%.0f%%", p.Utilization*100)
		_, _ = fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%s\n", p.Name, p.Workers, p.ActiveJobs, p.Capacity, util)
	}
	_ = w.Flush()
}

func poolGet(args []string) {
	fs := newFlagSet("pool get")
	fs.ParseArgs(args)
	if fs.NArg() < 1 {
		fail("pool name required")
	}
	client := newClientFromFlags(fs)

	pool, err := client.GetPool(context.Background(), fs.Arg(0))
	check(err)
	printJSON(pool)
}

func poolCreate(args []string) {
	fs := newFlagSet("pool create")
	requires := fs.String("requires", "", "comma-separated capability requirements")
	description := fs.String("description", "", "pool description")
	fs.ParseArgs(args)
	if fs.NArg() < 1 {
		fail("pool name required")
	}
	name := fs.Arg(0)
	client := newClientFromFlags(fs)

	req := &sdk.PoolCreateRequest{
		Description: *description,
	}
	if *requires != "" {
		req.Requires = strings.Split(*requires, ",")
		for i := range req.Requires {
			req.Requires[i] = strings.TrimSpace(req.Requires[i])
		}
	}
	check(client.CreatePool(context.Background(), name, req))
	fmt.Printf("Pool %q created\n", name)
}

func poolUpdate(args []string) {
	fs := newFlagSet("pool update")
	requires := fs.String("requires", "", "comma-separated capability requirements")
	description := fs.String("description", "", "pool description")
	fs.ParseArgs(args)
	if fs.NArg() < 1 {
		fail("pool name required")
	}
	name := fs.Arg(0)
	client := newClientFromFlags(fs)

	req := &sdk.PoolUpdateRequest{}
	if *requires != "" {
		reqs := strings.Split(*requires, ",")
		for i := range reqs {
			reqs[i] = strings.TrimSpace(reqs[i])
		}
		req.Requires = &reqs
	}
	if *description != "" {
		req.Description = description
	}
	check(client.UpdatePool(context.Background(), name, req))
	fmt.Printf("Pool %q updated\n", name)
}

func poolDelete(args []string) {
	fs := newFlagSet("pool delete")
	force := fs.Bool("force", false, "force delete even if pool has topic mappings")
	fs.ParseArgs(args)
	if fs.NArg() < 1 {
		fail("pool name required")
	}
	name := fs.Arg(0)
	client := newClientFromFlags(fs)

	check(client.DeletePool(context.Background(), name, *force))
	fmt.Printf("Pool %q deleted\n", name)
}

func poolDrain(args []string) {
	fs := newFlagSet("pool drain")
	timeout := fs.Int("timeout", 300, "drain timeout in seconds")
	fs.ParseArgs(args)
	if fs.NArg() < 1 {
		fail("pool name required")
	}
	name := fs.Arg(0)
	client := newClientFromFlags(fs)

	check(client.DrainPool(context.Background(), name, &sdk.PoolDrainRequest{
		TimeoutSeconds: *timeout,
	}))
	fmt.Printf("Pool %q draining (timeout: %ds)\n", name, *timeout)
}

func poolTopic(args []string) {
	if len(args) < 1 {
		fail("usage: cordumctl pool topic <add|remove> <pool> <topic>")
	}
	switch args[0] {
	case "add":
		poolTopicAdd(args[1:])
	case "remove":
		poolTopicRemove(args[1:])
	default:
		fail("usage: cordumctl pool topic <add|remove> <pool> <topic>")
	}
}

func poolTopicAdd(args []string) {
	fs := newFlagSet("pool topic add")
	fs.ParseArgs(args)
	if fs.NArg() < 2 {
		fail("usage: cordumctl pool topic add <pool> <topic>")
	}
	pool, topic := fs.Arg(0), fs.Arg(1)
	client := newClientFromFlags(fs)

	check(client.AddTopicToPool(context.Background(), pool, topic))
	fmt.Printf("Topic %q added to pool %q\n", topic, pool)
}

func poolTopicRemove(args []string) {
	fs := newFlagSet("pool topic remove")
	fs.ParseArgs(args)
	if fs.NArg() < 2 {
		fail("usage: cordumctl pool topic remove <pool> <topic>")
	}
	pool, topic := fs.Arg(0), fs.Arg(1)
	client := newClientFromFlags(fs)

	check(client.RemoveTopicFromPool(context.Background(), pool, topic))
	fmt.Printf("Topic %q removed from pool %q\n", topic, pool)
}
