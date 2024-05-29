package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/Filecoin-Titan/titan/api/types"
	"github.com/Filecoin-Titan/titan/lib/tablewriter"
	"github.com/fatih/color"
	"github.com/urfave/cli/v2"
)

var projectCmds = &cli.Command{
	Name:  "project",
	Usage: "Manage project",
	Subcommands: []*cli.Command{
		deployProjectCmd,
		deleteProjectCmd,
		showProjectInfoCmd,
		listProjectCmd,
		restartProjectCmd,
		updateProjectCmd,
	},
}

var restartProjectCmd = &cli.Command{
	Name:  "restart",
	Usage: "publish restart project tasks to nodes",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "pid",
			Usage: "project id",
			Value: "",
		},
	},
	Action: func(cctx *cli.Context) error {
		pid := cctx.String("pid")

		ctx := ReqContext(cctx)
		schedulerAPI, closer, err := GetSchedulerAPI(cctx, "")
		if err != nil {
			return err
		}
		defer closer()

		info, err := schedulerAPI.GetProjectInfo(ctx, pid)
		if err != nil {
			return err
		}

		return schedulerAPI.RedeployFailedProjects(ctx, []string{info.UUID})
	},
}

var listProjectCmd = &cli.Command{
	Name:  "list",
	Usage: "List project",
	Flags: []cli.Flag{
		limitFlag,
		offsetFlag,
		&cli.StringFlag{
			Name:  "user",
			Usage: "user id",
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := ReqContext(cctx)
		schedulerAPI, closer, err := GetSchedulerAPI(cctx, "")
		if err != nil {
			return err
		}
		defer closer()

		limit := cctx.Int("limit")
		offset := cctx.Int("offset")
		user := cctx.String("user")

		tw := tablewriter.New(
			tablewriter.Col("Num"),
			tablewriter.Col("UUID"),
			tablewriter.Col("State"),
			tablewriter.Col("Name"),
			tablewriter.Col("UserID"),
			tablewriter.Col("BundleURL"),
			tablewriter.Col("Replicas"),
			tablewriter.Col("CreatedTime"),
			tablewriter.NewLineCol("Processes"),
		)

		list, err := schedulerAPI.GetProjectInfos(ctx, user, limit, offset)
		if err != nil {
			return err
		}

		for w := 0; w < len(list); w++ {
			info := list[w]

			m := map[string]interface{}{
				"Num":         w + 1,
				"UUID":        info.UUID,
				"State":       projectColorState(info.State),
				"Name":        info.Name,
				"UserID":      info.UserID,
				"BundleURL":   info.BundleURL,
				"Replicas":    info.Replicas,
				"CreatedTime": info.CreatedTime.Format(defaultDateTimeLayout),
			}

			processes := "\n"
			for j := 0; j < len(info.DetailsList); j++ {
				dInfo := info.DetailsList[j]

				status := projectColorState(dInfo.Status.String())
				processes += fmt.Sprintf("\t%s(%s): %s\n", dInfo.NodeID, status, dInfo.WsURL)
			}
			m["Processes"] = processes
			tw.Write(m)
		}

		tw.Flush(os.Stdout)
		return nil
	},
}

var deployProjectCmd = &cli.Command{
	Name:  "deploy",
	Usage: "deploy the project",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "name",
			Usage: "project name",
			Value: "",
		},
		&cli.StringFlag{
			Name:  "url",
			Usage: "project bundle url",
			Value: "",
		},
		&cli.StringFlag{
			Name:  "uid",
			Usage: "user id",
			Value: "",
		},
		replicaCountFlag,
	},
	Action: func(cctx *cli.Context) error {
		name := cctx.String("name")
		url := cctx.String("url")
		uid := cctx.String("uid")
		count := cctx.Int("replica-count")

		ctx := ReqContext(cctx)

		schedulerAPI, closer, err := GetSchedulerAPI(cctx, "")
		if err != nil {
			return err
		}
		defer closer()

		pid, err := schedulerAPI.DeployProject(ctx, &types.DeployProjectReq{Name: name, BundleURL: url, UserID: uid, Replicas: int64(count)})
		if err != nil {
			return err
		}

		fmt.Println("pid:", pid)

		return nil
	},
}

var deleteProjectCmd = &cli.Command{
	Name:  "delete",
	Usage: "delete the project",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "pid",
			Usage: "project id",
			Value: "",
		},
		&cli.StringFlag{
			Name:  "uid",
			Usage: "user id",
			Value: "",
		},
		nodeIDFlag,
	},
	Action: func(cctx *cli.Context) error {
		pid := cctx.String("pid")
		uid := cctx.String("uid")
		nid := cctx.String("node-id")

		ctx := ReqContext(cctx)

		schedulerAPI, closer, err := GetSchedulerAPI(cctx, "")
		if err != nil {
			return err
		}
		defer closer()

		return schedulerAPI.DeleteProject(ctx, &types.ProjectReq{UUID: pid, UserID: uid, NodeID: nid})
	},
}

var updateProjectCmd = &cli.Command{
	Name:  "update",
	Usage: "update the project",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "pid",
			Usage: "project id",
			Value: "",
		},
		&cli.StringFlag{
			Name:  "name",
			Usage: "project name",
			Value: "",
		},
		&cli.StringFlag{
			Name:  "url",
			Usage: "project bundle url",
			Value: "",
		},
		&cli.StringFlag{
			Name:  "uid",
			Usage: "user id",
			Value: "",
		},
		replicaCountFlag,
	},
	Action: func(cctx *cli.Context) error {
		pid := cctx.String("pid")
		name := cctx.String("name")
		url := cctx.String("url")
		uid := cctx.String("uid")
		count := cctx.Int("replica-count")

		ctx := ReqContext(cctx)

		schedulerAPI, closer, err := GetSchedulerAPI(cctx, "")
		if err != nil {
			return err
		}
		defer closer()

		return schedulerAPI.UpdateProject(ctx, &types.ProjectReq{Name: name, BundleURL: url, UserID: uid, Replicas: int64(count), UUID: pid})
	},
}

var showProjectInfoCmd = &cli.Command{
	Name:  "info",
	Usage: "Show the project info",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "pid",
			Usage: "project id",
			Value: "",
		},
	},
	Action: func(cctx *cli.Context) error {
		pid := cctx.String("pid")

		ctx := ReqContext(cctx)
		schedulerAPI, closer, err := GetSchedulerAPI(cctx, "")
		if err != nil {
			return err
		}
		defer closer()

		info, err := schedulerAPI.GetProjectInfo(ctx, pid)
		if err != nil {
			return err
		}

		fmt.Printf("UUID:\t%s\n", info.UUID)
		fmt.Printf("State:\t%s\n", projectColorState(info.State))
		fmt.Printf("Name:\t%s\n", info.Name)
		fmt.Printf("BundleURL:\t%s\n", info.BundleURL)
		fmt.Printf("UserID:\t%s\n", info.UserID)
		fmt.Printf("Replicas:\t%d\n", info.Replicas)

		for _, data := range info.DetailsList {
			fmt.Printf("%s(%s): %s\n", data.NodeID, projectColorState(data.Status.String()), data.WsURL)
		}

		return nil
	},
}

func projectColorState(state string) string {
	if strings.Contains(state, "Failed") || strings.Contains(state, "error") || strings.Contains(state, "stopped") || strings.Contains(state, "Remove") {
		return color.RedString(state)
	} else if strings.Contains(state, "Servicing") || strings.Contains(state, "Succeeded") || strings.Contains(state, "started") || strings.Contains(state, "Create") {
		return color.GreenString(state)
	} else {
		return color.YellowString(state)
	}
}
