webix.ready(function () {

    var tsFormat = webix.Date.dateToStr("%Y-%m-%d %H:%i:%s");
    var tsFormatter = function (s) {
        return tsFormat(new Date(s));
    };

    webix.ui({
        rows: [
            {
                view: "toolbar",
                height: 40,
                cols: [
                    {view: "button", type: "image", id: "home", image: "logo-32.png", width: 50},
                    {view: "datepicker", id: "fromTimestamp", timepicker: true, width: 200},
                    {view: "datepicker", id: "toTimestamp", timepicker: true, width: 200},
                    {view: "text", id: "message", width: 300},
                    {view: "checkbox", id: "follow", label: "Follow", value: true, width: 100},
                    {view: "button", id: "prevPage", value: "<<", width: 50},
                    {view: "button", id: "nextPage", value: ">>", width: 50}
                ]
            },
            {
                cols: [
                    {
                        view: "datatable",
                        id: "queryStat",
                        //autoConfig: true,
                        columns: [
                            {id: "Hostname", header: "Hostname", width: 150},
                            {id: "Application", header: "Application", width: 150},
                            {id: "Count", header: "Count", fillspace: true}
                        ],
                        select: "row",
                        data: [],
                        width: 300
                    },
                    {
                        view: "datatable",
                        id: "queryList",
                        //autoConfig: true,
                        columns: [
                            {id: "Timestamp", header: "Timestamp", width: 175, format: tsFormatter},
                            {id: "Hostname", header: "Hostname", width: 150},
                            {id: "Application", header: "Application", width: 150},
                            {id: "Message", header: "Message", fillspace: true}
                        ],
                        data: []
                    }
                ]
            }
        ]
    });

    function post(url, data) {
        return $.ajax(url, {
            method: "POST",
            contentType: "application/json",
            data: JSON.stringify(data),
            dataType: "json"
        })
    }

    //var home = $$("home");
    var fromTimestamp = $$("fromTimestamp");
    var toTimestamp = $$("toTimestamp");
    var message = $$("message");
    var follow = $$("follow");
    var prevPage = $$("prevPage");
    var nextPage = $$("nextPage");
    var queryStat = $$("queryStat");
    var queryList = $$("queryList");

    var queryStatRequest = {
        Limit: 500
    };

    var queryListRequest = {
        Limit: 50
    };

    var autoUpdateEnabled = true;

    var updateStat = function () {
        return post("api/stat", queryStatRequest).done(function (data) {
            var selectedId = queryStat.getSelectedId();
            queryStat.clearAll();
            queryStat.add({
                id: "*-*",
                Hostname: "*",
                Application: "*"
            });
            if (data.Stat) {
                $.each(data.Stat, function (hostname, applications) {
                    queryStat.add({
                        id: hostname + "-*",
                        Hostname: hostname,
                        Application: "*"
                    });
                    $.each(applications, function (application, count) {
                        queryStat.add({
                            id: hostname + "-" + application,
                            Hostname: hostname,
                            Application: application,
                            Count: count
                        });
                    });
                });
            }
            queryStat.adjustColumn("Hostname");
            queryStat.adjustColumn("Application");
            queryStat.adjustColumn("Count");
            if (selectedId) {
                try {
                    queryStat.select(selectedId);
                } catch (e) {
                    queryStat.select(queryStat.getIdByIndex(0));
                }
            }
        });
    };

    var updateList = function () {
        return post("api/list", queryListRequest).done(function (data) {
            queryList.clearAll();
            if (data.Entries) {
                data.Entries = data.Entries.reverse();
                $(data.Entries).each(function (_, v) {
                    queryList.add(v);
                });
            }
            queryList.adjustColumn("Timestamp");
            queryList.adjustColumn("Hostname");
            queryList.adjustColumn("Application");
            if (data.Entries) {
                queryList.showItemByIndex(data.Entries.length);
            }
        });
    };

    var autoUpdate = function () {
        setTimeout(function () {
            if (autoUpdateEnabled) {
                updateStat().always(autoUpdate);
            } else {
                autoUpdate();
            }
        }, 5000);
    };

    fromTimestamp.attachEvent("onChange", function (value) {
        queryStatRequest.FromTimestamp = queryListRequest.FromTimestamp = value;
        queryListRequest.Offset = 0;
        updateStat();
    });

    toTimestamp.attachEvent("onChange", function (value) {
        queryStatRequest.ToTimestamp = queryListRequest.ToTimestamp = value;
        queryListRequest.Offset = 0;
        updateStat();
    });

    message.attachEvent("onChange", function (value) {
        queryStatRequest.Message = queryListRequest.Message = value;
        queryListRequest.Offset = 0;
        updateStat();
    });

    follow.attachEvent("onChange", function (value) {
        autoUpdateEnabled = value;
        if (value === true) {
            queryListRequest.Offset = 0;
            updateStat();
        }
    });

    prevPage.attachEvent("onItemClick", function () {
        follow.setValue(false);
        if (!queryListRequest.Offset) {
            queryListRequest.Offset = 0;
        }
        queryListRequest.Offset += queryListRequest.Limit;
        updateList();
    });

    nextPage.attachEvent("onItemClick", function () {
        if (follow.getValue()) {
            follow.setValue(false);
        }
        if (!queryListRequest.Offset) {
            queryListRequest.Offset = 0;
        }
        queryListRequest.Offset -= queryListRequest.Limit;
        if (queryListRequest.Offset <= 0) {
            queryListRequest.Offset = 0;
            follow.setValue(true);
        } else {
            updateList();
        }
    });

    queryStat.attachEvent("onAfterSelect", function (data) {
        if (data && data.id) {
            var stat = queryStat.getItem(data.id);
            if (stat) {
                queryListRequest.Hostname = stat.Hostname !== "*" ? stat.Hostname : null;
                queryListRequest.Application = stat.Application !== "*" ? stat.Application : null;
            }
        }
        queryListRequest.Offset = 0;
        updateList();
    });

    updateStat().done(function () {
        queryStat.select(queryStat.getIdByIndex(0));
        autoUpdate();
    });

});