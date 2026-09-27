// Native UI for the separately packaged reference coordinator.
// Uses only the host React, UI components, action transport, and public context.
(function () {
  var PLUGIN_ID = "kandev-plugin-coordinator-template";
  var HOME_PATH = "/plugins/kandev-plugin-coordinator-template";
  var SETTINGS_PATH = "/plugins/kandev-plugin-coordinator-template/settings";

  function action(host, workspaceId, key, body) {
    return host.api.invokeAction(key, { workspaceId: workspaceId, body: body || {} });
  }

  function accepted(result) {
    if (!result || ["APPLIED", "ALREADY_APPLIED", "NO_CHANGE"].indexOf(result.status) === -1) {
      throw new Error((result && result.reason) || "The Host did not accept this command");
    }
  }

  function makeConversationPage(host) {
    var React = host.React;
    var h = host.jsx;
    var ui = host.ui;

    function ConversationPage() {
      var activeWorkspace = React.useState(host.context.getActiveWorkspaceId() || "");
      var workspaceId = activeWorkspace[0];
      var setWorkspaceId = activeWorkspace[1];
      var instanceState = React.useState([]);
      var instances = instanceState[0];
      var setInstances = instanceState[1];
      var selectedState = React.useState("");
      var selectedKey = selectedState[0];
      var setSelectedKey = selectedState[1];
      var selectedRef = React.useRef(selectedKey);
      var selectionGenerationRef = React.useRef(0);
      var snapshotState = React.useState({
        workspaceId: workspaceId,
        instanceKey: "",
        taskId: "",
        sessionId: null,
        revision: 0,
        sessionResourceVersion: "",
        state: "loading",
        pendingInteractions: [],
      });
      var snapshot = snapshotState[0];
      var setSnapshot = snapshotState[1];
      var taskState = React.useState([]);
      var taskStatuses = taskState[0];
      var setTaskStatuses = taskState[1];
      var policyState = React.useState(null);
      var policy = policyState[0];
      var setPolicy = policyState[1];
      var outcomesState = React.useState({ reports: [], uncertainWrites: [] });
      var outcomes = outcomesState[0];
      var setOutcomes = outcomesState[1];
      var taskInputsState = React.useState({});
      var taskInputs = taskInputsState[0];
      var setTaskInputs = taskInputsState[1];
      var issueCapabilitiesState = React.useState({});
      var issueCapabilities = issueCapabilitiesState[0];
      var setIssueCapabilities = issueCapabilitiesState[1];
      var issueWriteStatesState = React.useState({});
      var issueWriteStates = issueWriteStatesState[0];
      var setIssueWriteStates = issueWriteStatesState[1];
      var taskTitleState = React.useState("");
      var taskTitle = taskTitleState[0];
      var setTaskTitle = taskTitleState[1];
      var externalTaskIdState = React.useState("");
      var externalTaskId = externalTaskIdState[0];
      var setExternalTaskId = externalTaskIdState[1];
      var taskDescriptionState = React.useState("");
      var taskDescription = taskDescriptionState[0];
      var setTaskDescription = taskDescriptionState[1];
      var proposalState = React.useState([]);
      var proposals = proposalState[0];
      var setProposals = proposalState[1];
      var proposalTitleState = React.useState("");
      var proposalTitle = proposalTitleState[0];
      var setProposalTitle = proposalTitleState[1];
      var proposalDescriptionState = React.useState("");
      var proposalDescription = proposalDescriptionState[0];
      var setProposalDescription = proposalDescriptionState[1];
      var errorState = React.useState("");
      var error = errorState[0];
      var setError = errorState[1];

      React.useEffect(function () {
        var unsubscribe = host.context.subscribeActiveWorkspace(function (next) {
          setWorkspaceId(next || "");
          selectionGenerationRef.current += 1;
          setSelectedKey("");
          selectedRef.current = "";
          setInstances([]);
          setTaskStatuses([]);
        });
        return unsubscribe;
      }, []);

      function refreshInstances() {
        if (!workspaceId) return Promise.resolve([]);
        return action(host, workspaceId, "instance.list").then(function (response) {
          var next = response.instances || [];
          setInstances(next);
          var current = selectedRef.current;
          if (!current || !next.some(function (item) { return item.key === current; })) {
            current = next.length ? next[0].key : "";
            selectedRef.current = current;
            setSelectedKey(current);
          }
          return next;
        });
      }

      function readStatus(instanceKey, selectionGeneration) {
        if (!workspaceId || !instanceKey) {
          return Promise.resolve({
            workspaceId: workspaceId,
            instanceKey: instanceKey || "",
            taskId: "",
            sessionId: null,
            revision: 0,
            sessionResourceVersion: "",
            state: "loading",
            pendingInteractions: [],
          });
        }
        return action(host, workspaceId, "conversation.status", { instance_key: instanceKey }).then(function (next) {
          if (selectedRef.current === instanceKey && selectionGenerationRef.current === selectionGeneration) setSnapshot(next);
          return next;
        });
      }

      function refreshTaskStatuses(instanceKey) {
        if (!workspaceId || !instanceKey) {
          setTaskStatuses([]);
          return Promise.resolve([]);
        }
        return action(host, workspaceId, "task.status", { instance_key: instanceKey }).then(function (response) {
          if (selectedRef.current === instanceKey) {
            setTaskStatuses(response.tasks || []);
            setPolicy(response.policy || null);
          }
          return response.tasks || [];
        }).catch(function () {
          if (selectedRef.current === instanceKey) setTaskStatuses([]);
          return [];
        });
      }

      function refreshProposals(instanceKey) {
        if (!workspaceId || !instanceKey) {
          setProposals([]);
          return Promise.resolve([]);
        }
        return action(host, workspaceId, "proposal.list", { instance_key: instanceKey }).then(function (response) {
          var next = response.proposals || [];
          if (selectedRef.current === instanceKey) setProposals(next);
          return next;
        }).catch(function () {
          if (selectedRef.current === instanceKey) setProposals([]);
          return [];
        });
      }

      function refreshOutcomes(instanceKey) {
        if (!workspaceId || !instanceKey) {
          setOutcomes({ reports: [], uncertainWrites: [] });
          return Promise.resolve({ reports: [], uncertainWrites: [] });
        }
        return action(host, workspaceId, "outcome.list", { instance_key: instanceKey }).then(function (response) {
          if (selectedRef.current === instanceKey) setOutcomes({ reports: response.reports || [], uncertainWrites: response.uncertainWrites || [] });
          return response;
        }).catch(function () {
          if (selectedRef.current === instanceKey) setOutcomes({ reports: [], uncertainWrites: [] });
          return { reports: [], uncertainWrites: [] };
        });
      }

      function refreshIssueWrites(instanceKey) {
        if (!workspaceId || !instanceKey) return Promise.resolve([]);
        return action(host, workspaceId, "issue.writes.list", { instance_key: instanceKey }).then(function (response) {
          var writes = response.writes || [];
          if (selectedRef.current === instanceKey) setIssueWriteStates(writes);
          return writes;
        }).catch(function () { return []; });
      }

      function selectInstance(instanceKey) {
        var selectionGeneration = selectionGenerationRef.current + 1;
        selectionGenerationRef.current = selectionGeneration;
        selectedRef.current = instanceKey;
        setSelectedKey(instanceKey);
        setSnapshot({
          workspaceId: workspaceId,
          instanceKey: instanceKey,
          taskId: "",
          sessionId: null,
          revision: 0,
          sessionResourceVersion: "",
          state: "loading",
          pendingInteractions: [],
        });
        setError("");
        return Promise.all([readStatus(instanceKey, selectionGeneration), refreshTaskStatuses(instanceKey), refreshProposals(instanceKey), refreshOutcomes(instanceKey), refreshIssueWrites(instanceKey)]).catch(function (reason) {
          setError(reason.message || "Could not load this coordinator instance");
        });
      }

      React.useEffect(function () {
        if (!workspaceId) return;
        refreshInstances().then(function (next) {
          var key = selectedRef.current || (next[0] && next[0].key) || "";
          if (key) return selectInstance(key);
          setSnapshot({
            workspaceId: workspaceId,
            instanceKey: "",
            taskId: "",
            sessionId: null,
            revision: 0,
            sessionResourceVersion: "",
            state: "loading",
            pendingInteractions: [],
          });
        }).catch(function (reason) {
          setError(reason.message || "Could not load coordinator instances");
        });
      }, [workspaceId]);

      React.useEffect(function () {
        selectedRef.current = selectedKey;
      }, [selectedKey]);

      function invokeConversation(instanceKey, key, body) {
        return action(host, workspaceId, key, Object.assign({ instance_key: instanceKey }, body || {}));
      }

      function controllerFor(instanceKey) {
        var selectionGeneration = selectionGenerationRef.current;
        return {
        getStatus: function () { return readStatus(instanceKey, selectionGeneration); },
        listInputs: function (input) {
          return invokeConversation(instanceKey, "conversation.inputs", {
            sequence_cursor: input.sequenceCursor,
            limit: input.limit,
          }).then(function (response) {
            return {
              inputs: response.inputs || [],
              nextSequenceCursor: response.nextSequenceCursor || 0,
              hasMore: response.hasMore || false,
            };
          });
        },
        enqueue: function (input) {
          return invokeConversation(instanceKey, "conversation.enqueue", {
            request_id: input.requestId,
            idempotency_key: input.idempotencyKey,
            expected_revision: input.expectedConversationRevision,
            occurrence_key: input.occurrenceKey,
            payload: input.payload,
          }).then(function (response) {
            accepted(response);
            return { receipt: response.input };
          });
        },
        cancelInput: function (input) {
          return invokeConversation(instanceKey, "conversation.cancel", {
            request_id: input.requestId,
            idempotency_key: input.idempotencyKey,
            expected_revision: input.expectedConversationRevision,
            host_input_id: input.hostInputId,
            expected_execution_id: input.expectedExecutionId,
          }).then(function (response) {
            accepted(response);
            return { receipt: response.input };
          });
        },
        setPaused: function (input) {
          return invokeConversation(instanceKey, "conversation.pause", {
            request_id: input.requestId,
            idempotency_key: input.idempotencyKey,
            expected_revision: input.expectedConversationRevision,
            paused: input.paused,
          }).then(function (response) {
            accepted(response);
            return readStatus(instanceKey, selectionGeneration);
          });
        },
        recover: function (input) {
          return invokeConversation(instanceKey, "conversation.recover", {
            request_id: input.requestId,
            idempotency_key: input.idempotencyKey,
            expected_revision: input.expectedConversationRevision,
            expected_session_resource_version: input.expectedSessionResourceVersion,
            expected_execution_id: input.expectedExecutionId,
          }).then(function (response) {
            accepted(response);
            return readStatus(instanceKey, selectionGeneration);
          });
        },
        respondToPermission: function (input) {
          return invokeConversation(instanceKey, "conversation.permission-response", {
            request_id: input.requestId,
            interaction_id: input.interactionId,
            expected_resource_version: input.expectedResourceVersion,
            option_id: input.optionId,
            cancelled: input.cancelled,
            human_response_receipt_id: input.humanResponseReceiptId,
          }).then(accepted);
        },
        answerClarification: function (input) {
          return invokeConversation(instanceKey, "conversation.clarification-response", {
            request_id: input.requestId,
            interaction_id: input.interactionId,
            expected_resource_version: input.expectedResourceVersion,
            answers: input.answers.map(function (answer) {
              return {
                question_id: answer.questionId,
                selected_options: answer.selectedOptions,
                custom_text: answer.customText,
              };
            }),
            human_response_receipt_id: input.humanResponseReceiptId,
          }).then(accepted);
        },
        };
      }

      var controller = controllerFor(selectedKey);

      function taskFields(taskId) {
        return taskInputs[taskId] || {};
      }

      function patchTaskField(taskId, field, value) {
        var next = Object.assign({}, taskInputs);
        next[taskId] = Object.assign({}, next[taskId] || {}, (function () { var patch = {}; patch[field] = value; return patch; })());
        setTaskInputs(next);
      }

      function refreshTaskOperationData(instanceKey) {
        return Promise.all([refreshTaskStatuses(instanceKey), refreshIssueWrites(instanceKey), refreshOutcomes(instanceKey)]);
      }

      function adoptTask(task) {
        action(host, workspaceId, "task.adopt", {
          instance_key: selectedKey,
          task_id: task.taskId,
          request_id: host.utils.generateUUID(),
          reason: "Coordinator instance accepted management responsibility",
        }).then(function (response) {
          accepted(response);
          return refreshTaskOperationData(selectedKey);
        }).catch(function (reason) { setError(reason.message || "Could not adopt the task"); });
      }

      function adoptExternalTask() {
        var taskId = externalTaskId.trim();
        if (!taskId || !selectedKey) return;
        action(host, workspaceId, "task.adopt", {
          instance_key: selectedKey,
          task_id: taskId,
          request_id: host.utils.generateUUID(),
          reason: "User selected this workspace task for coordinator management",
        }).then(function (response) {
          accepted(response);
          setExternalTaskId("");
          return Promise.all([refreshInstances(), refreshTaskOperationData(selectedKey)]);
        }).catch(function (reason) { setError(reason.message || "Could not adopt the selected task"); });
      }

      function releaseTask(task) {
        action(host, workspaceId, "task.release", {
          instance_key: selectedKey,
          task_id: task.taskId,
          request_id: host.utils.generateUUID(),
          reason: "Coordinator instance released management responsibility",
        }).then(function (response) {
          accepted(response);
          return refreshTaskOperationData(selectedKey);
        }).catch(function (reason) { setError(reason.message || "Could not release the task claim"); });
      }

      function saveCompletionCriterion(task, fields) {
        action(host, workspaceId, "completion.criteria", {
          instance_key: selectedKey,
          task_id: task.taskId,
          request_id: host.utils.generateUUID(),
          criteria: [{
            id: (fields.criterionId || "").trim(),
            description: (fields.criterionDescription || "").trim(),
            evidence_kind: (fields.evidenceKind || "").trim(),
            evidence_id: (fields.evidenceId || "").trim(),
            evidence_revision: (fields.evidenceRevision || "").trim(),
          }],
        }).then(function (response) {
          accepted(response);
          return refreshTaskStatuses(selectedKey);
        }).catch(function (reason) { setError(reason.message || "Could not save completion criteria"); });
      }

      function submitCompletionEvidence(task, fields) {
        action(host, workspaceId, "completion.evidence", {
          instance_key: selectedKey,
          task_id: task.taskId,
          criterion_id: (fields.criterionId || "").trim(),
          request_id: host.utils.generateUUID(),
          evidence_kind: (fields.evidenceKind || "").trim(),
          evidence_id: (fields.evidenceId || "").trim(),
          evidence_revision: (fields.evidenceRevision || "").trim(),
          summary: (fields.evidenceSummary || "").trim(),
          reference: (fields.evidenceReference || "").trim(),
        }).then(function (response) {
          accepted(response);
          return refreshTaskOperationData(selectedKey);
        }).catch(function (reason) { setError(reason.message || "Could not submit completion evidence"); });
      }

      function readIssueCapabilities(task) {
        action(host, workspaceId, "issue.capabilities", { task_id: task.taskId }).then(function (response) {
          var next = Object.assign({}, issueCapabilities);
          next[task.taskId] = response;
          setIssueCapabilities(next);
          return refreshIssueWrites(selectedKey);
        }).catch(function (reason) { setError(reason.message || "Could not read linked issue capabilities"); });
      }

      function writeIssueComment(task, fields) {
        action(host, workspaceId, "issue.comment", {
          instance_key: selectedKey,
          task_id: task.taskId,
          request_id: host.utils.generateUUID(),
          body: (fields.issueComment || "").trim(),
        }).then(function (response) {
          accepted(response);
          return refreshIssueWrites(selectedKey);
        }).then(function () { patchTaskField(task.taskId, "issueComment", ""); }).catch(function (reason) {
          setError(reason.message || "Linked issue result may be uncertain. Check the write receipt before retrying.");
          return refreshIssueWrites(selectedKey);
        });
      }

      function transitionIssue(task, fields) {
        action(host, workspaceId, "issue.transition", {
          instance_key: selectedKey,
          task_id: task.taskId,
          request_id: host.utils.generateUUID(),
          target_id: fields.transitionTarget || "",
        }).then(function (response) {
          accepted(response);
          return Promise.all([readIssueCapabilities(task), refreshIssueWrites(selectedKey)]);
        }).catch(function (reason) { setError(reason.message || "Linked issue result may be uncertain. Check the write receipt before retrying."); });
      }

      function taskList() {
        if (!taskStatuses.length) {
          return h("p", { className: "text-sm text-muted-foreground" }, "No delegated tasks yet.");
        }
        return h("div", { className: "space-y-3", "data-testid": "coordinator-task-list" }, taskStatuses.map(function (task) {
          var fields = taskFields(task.taskId);
          var issue = issueCapabilities[task.taskId];
          var issueLink = issue && issue.capabilities;
          var commentPermission = issue && issue.writeback && issue.writeback.comment;
          var transitionPermission = issue && issue.writeback && issue.writeback.transition;
          var relatedWrites = issueWriteStates.filter(function (write) { return write.taskId === task.taskId; });
          return h("article", { key: task.taskId, className: "space-y-3 rounded-md border p-3", "data-task-id": task.taskId },
            h("div", null,
              h("h3", { className: "font-medium" }, task.title || task.taskId),
              h("p", { className: "mt-1 text-xs text-muted-foreground" }, "Canonical status: " + (task.canonicalStatus || "unknown")),
              task.statusKnown === false ? h("p", { className: "text-xs text-amber-700" }, "Host status is unavailable") : null,
              task.blockingReasons && task.blockingReasons.length ? h("p", { className: "text-xs text-amber-700", "data-testid": "coordinator-task-blockers" }, "Host blockers: " + task.blockingReasons.join(", ")) : null,
              task.lastCoordinatorClaimGeneration ? h("p", { className: "text-xs text-muted-foreground" }, "Last coordinator claim receipt: generation " + task.lastCoordinatorClaimGeneration) : null,
              h(ui.WorkspaceTaskStatus, { taskId: task.taskId }),
            ),
            h("details", { className: "rounded-md border p-2", "data-testid": "coordinator-task-operations" },
              h("summary", { className: "min-h-11 cursor-pointer py-2 font-medium" }, "Management and completion"),
              h("div", { className: "space-y-3 pt-2" },
                h("div", { className: "flex flex-wrap gap-2" },
                  h(ui.Button, { type: "button", variant: "outline", className: "min-h-11", onClick: function () { adoptTask(task); }, "data-testid": "coordinator-adopt-task" }, "Adopt task"),
                  task.lastCoordinatorClaimGeneration ? h(ui.Button, { type: "button", variant: "outline", className: "min-h-11", onClick: function () { releaseTask(task); }, "data-testid": "coordinator-release-task" }, "Release claim") : null,
                ),
                h("p", { className: "text-xs text-muted-foreground" }, "Adoption is explicit. The Host checks the current claim and task version for every command."),
                h("label", { className: "block space-y-1 text-sm" }, h("span", null, "Criterion ID"), h("input", { className: "min-h-11 w-full rounded-md border bg-background px-3", value: fields.criterionId || "", onChange: function (event) { patchTaskField(task.taskId, "criterionId", event.target.value); }, "data-testid": "coordinator-criterion-id" })),
                h("label", { className: "block space-y-1 text-sm" }, h("span", null, "Completion requirement"), h("input", { className: "min-h-11 w-full rounded-md border bg-background px-3", value: fields.criterionDescription || "", onChange: function (event) { patchTaskField(task.taskId, "criterionDescription", event.target.value); }, "data-testid": "coordinator-criterion-description" })),
                h("div", { className: "grid gap-2 sm:grid-cols-3" },
                  h("label", { className: "block space-y-1 text-sm" }, h("span", null, "Evidence kind"), h("input", { className: "min-h-11 w-full rounded-md border bg-background px-3", value: fields.evidenceKind || "", onChange: function (event) { patchTaskField(task.taskId, "evidenceKind", event.target.value); }, "data-testid": "coordinator-evidence-kind" })),
                  h("label", { className: "block space-y-1 text-sm" }, h("span", null, "Evidence ID"), h("input", { className: "min-h-11 w-full rounded-md border bg-background px-3", value: fields.evidenceId || "", onChange: function (event) { patchTaskField(task.taskId, "evidenceId", event.target.value); }, "data-testid": "coordinator-evidence-id" })),
                  h("label", { className: "block space-y-1 text-sm" }, h("span", null, "Evidence revision"), h("input", { className: "min-h-11 w-full rounded-md border bg-background px-3", value: fields.evidenceRevision || "", onChange: function (event) { patchTaskField(task.taskId, "evidenceRevision", event.target.value); }, "data-testid": "coordinator-evidence-revision" })),
                ),
                h("div", { className: "flex flex-wrap gap-2" },
                  h(ui.Button, { type: "button", variant: "outline", className: "min-h-11", disabled: !task.lastCoordinatorClaimGeneration || !fields.criterionId || !fields.criterionDescription || !fields.evidenceKind || !fields.evidenceId || !fields.evidenceRevision, onClick: function () { saveCompletionCriterion(task, fields); }, "data-testid": "coordinator-save-criterion" }, "Save completion criterion"),
                ),
                h("label", { className: "block space-y-1 text-sm" }, h("span", null, "Evidence summary"), h("textarea", { className: "min-h-20 w-full rounded-md border bg-background p-3", value: fields.evidenceSummary || "", onChange: function (event) { patchTaskField(task.taskId, "evidenceSummary", event.target.value); }, "data-testid": "coordinator-evidence-summary" })),
                h("label", { className: "block space-y-1 text-sm" }, h("span", null, "Evidence reference"), h("input", { className: "min-h-11 w-full rounded-md border bg-background px-3", value: fields.evidenceReference || "", onChange: function (event) { patchTaskField(task.taskId, "evidenceReference", event.target.value); }, "data-testid": "coordinator-evidence-reference" })),
                h(ui.Button, { type: "button", className: "min-h-11 w-full sm:w-auto", disabled: !task.lastCoordinatorClaimGeneration || !fields.criterionId || !fields.evidenceSummary || !fields.evidenceKind || !fields.evidenceId || !fields.evidenceRevision, onClick: function () { submitCompletionEvidence(task, fields); }, "data-testid": "coordinator-submit-evidence" }, "Submit completion evidence"),
                h("div", { className: "space-y-2 border-t pt-3" },
                  h("h4", { className: "font-medium" }, "Linked issue"),
                  h(ui.Button, { type: "button", variant: "outline", className: "min-h-11", onClick: function () { readIssueCapabilities(task); }, "data-testid": "coordinator-read-issue" }, "Check linked issue"),
                  issueLink ? h("div", { className: "space-y-2", "data-testid": "coordinator-linked-issue" },
                    h("a", { href: issueLink.url, target: "_blank", rel: "noreferrer", className: "text-sm underline" }, issueLink.identifier + " · " + issueLink.title),
                    h("p", { className: "text-xs text-muted-foreground" }, "Issue status: " + (issueLink.statusName || "unknown")),
                    h("label", { className: "block space-y-1 text-sm" }, h("span", null, "Comment"), h("textarea", { className: "min-h-20 w-full rounded-md border bg-background p-3", value: fields.issueComment || "", onChange: function (event) { patchTaskField(task.taskId, "issueComment", event.target.value); }, "data-testid": "coordinator-issue-comment" })),
                    h(ui.Button, { type: "button", variant: "outline", className: "min-h-11", disabled: !commentPermission || !commentPermission.authorized || !fields.issueComment || !fields.issueComment.trim(), onClick: function () { writeIssueComment(task, fields); }, "data-testid": "coordinator-issue-comment-submit" }, "Post comment"),
                    h("label", { className: "block space-y-1 text-sm" }, h("span", null, "Transition"), h("select", { className: "min-h-11 w-full rounded-md border bg-background px-3", value: fields.transitionTarget || "", onChange: function (event) { patchTaskField(task.taskId, "transitionTarget", event.target.value); }, "data-testid": "coordinator-issue-transition-target" },
                      [h("option", { key: "", value: "" }, "Choose a transition")].concat((issueLink.transitions || []).map(function (target) { return h("option", { key: target.targetId, value: target.targetId }, target.targetName); })),
                    )),
                    h(ui.Button, { type: "button", variant: "outline", className: "min-h-11", disabled: !transitionPermission || !transitionPermission.authorized || !fields.transitionTarget, onClick: function () { transitionIssue(task, fields); }, "data-testid": "coordinator-issue-transition-submit" }, "Update issue status"),
                    commentPermission && !commentPermission.authorized || transitionPermission && !transitionPermission.authorized ? h("p", { className: "text-xs text-amber-700" }, "Issue writes need a separate workspace capability grant.") : null,
                  ) : null,
                  relatedWrites.length ? h("div", { className: "space-y-2", "data-testid": "coordinator-issue-write-receipts" }, relatedWrites.map(function (write) {
                    var retryable = ["pending", "dispatching", "uncertain"].indexOf(String(write.status).toLowerCase()) !== -1;
                    return h("div", { key: write.requestId, className: "space-y-1" },
                      h("p", { className: "text-xs text-muted-foreground" }, "Issue write " + write.status + (write.lastError ? " · " + write.lastError : "")),
                      retryable ? h(ui.Button, { type: "button", variant: "outline", className: "min-h-11", onClick: function () {
                        action(host, workspaceId, "issue.write.retry", { instance_key: selectedKey, request_id: write.requestId }).then(function (response) {
                          accepted(response);
                          return refreshTaskOperationData(selectedKey);
                        }).catch(function (reason) { setError(reason.message || "Could not reconcile the linked issue write"); });
                      }, "data-testid": "coordinator-retry-issue-write" }, "Retry same issue write") : null,
                    );
                  })) : null,
                ),
              ),
            ),
          );
        }));
      }

      function taskOutcomes() {
        return h("div", { className: "space-y-3", "data-testid": "coordinator-task-outcomes" },
          h("div", { className: "flex flex-wrap items-center justify-between gap-2" },
            h("h2", { className: "font-semibold" }, "Outcomes"),
            h(ui.Button, { type: "button", variant: "outline", className: "min-h-11", disabled: !selectedKey, onClick: function () {
              action(host, workspaceId, "outcome.report", { instance_key: selectedKey }).then(function () { return refreshOutcomes(selectedKey); }).catch(function (reason) { setError(reason.message || "Could not refresh outcome reports"); });
            }, "data-testid": "coordinator-refresh-outcomes" }, "Refresh outcome report"),
          ),
          outcomes.reports.length ? outcomes.reports.map(function (report) {
            return h("article", { key: report.taskId, className: "space-y-1 rounded-md border p-3", "data-testid": "coordinator-outcome-report", "data-task-id": report.taskId },
              h("h3", { className: "font-medium" }, report.taskId),
              h("p", { className: "text-sm" }, "Task status: " + report.status + " · " + (report.verified ? "verified by Host" : "not verified by Host")),
              h("p", { className: "text-sm" }, "Usage: " + (report.costUsd == null ? "unknown" : "USD " + Number(report.costUsd).toFixed(2)) + " · " + report.costState),
              h("p", { className: "text-xs text-muted-foreground" }, "Evidence: " + report.provenance),
            );
          }) : h("p", { className: "text-sm text-muted-foreground" }, "No outcome report is available yet."),
          outcomes.uncertainWrites.length ? h("section", { className: "space-y-2 rounded-md border border-amber-500/40 p-3", "data-testid": "coordinator-uncertain-issue-writes" },
            h("h3", { className: "font-medium" }, "Linked issue writes need reconciliation"),
            outcomes.uncertainWrites.map(function (write) { return h("p", { key: write.requestId, className: "text-sm" }, write.taskId + " · " + write.status + (write.lastError ? " · " + write.lastError : "")); }),
          ) : null,
          taskStatuses.map(function (task) { return h(ui.WorkspaceTaskUsage, { key: task.taskId, taskId: task.taskId }); }),
        );
      }

      function delegateTask() {
        var title = taskTitle.trim();
        if (!title || !selectedKey) return;
        setError("");
        action(host, workspaceId, "task.delegate", {
          instance_key: selectedKey,
          request_id: host.utils.generateUUID(),
          title: title,
          description: taskDescription,
        }).then(function (response) {
          accepted(response);
          setTaskTitle("");
          setTaskDescription("");
          return Promise.all([refreshInstances(), refreshTaskStatuses(selectedKey)]);
        }).catch(function (reason) {
          setError(reason.message || "Could not delegate the task");
        });
      }

      function createProposal() {
        if (!proposalTitle.trim() || !selectedKey) return;
        action(host, workspaceId, "proposal.create", {
          instance_key: selectedKey,
          source_id: "ui:" + host.utils.generateUUID(),
          title: proposalTitle.trim(),
          description: proposalDescription,
        }).then(function () {
          setProposalTitle("");
          setProposalDescription("");
          return refreshProposals(selectedKey);
        }).catch(function (reason) {
          setError(reason.message || "Could not create a proposal");
        });
      }

      function decideProposal(proposal, decision) {
        action(host, workspaceId, "proposal." + decision, {
          proposal_id: proposal.proposalId,
          revision: proposal.revision,
        }).then(function () {
          return Promise.all([refreshProposals(selectedKey), refreshInstances(), refreshTaskStatuses(selectedKey)]);
        }).catch(function (reason) {
          setError(reason.message || "Could not update proposal");
        });
      }

      function reconcileProposal(proposal) {
        action(host, workspaceId, "proposal.reconcile", { instance_key: selectedKey }).then(function () {
          return Promise.all([refreshProposals(selectedKey), refreshInstances(), refreshTaskStatuses(selectedKey)]);
        }).catch(function (reason) {
          setError(reason.message || "Could not reconcile the proposal receipt");
          return refreshProposals(selectedKey);
        });
      }

      var proposalPanel = h("section", { className: "space-y-3 rounded-md border p-3", "data-testid": "coordinator-proposals" },
        h("h2", { className: "font-semibold" }, "Proposals"),
        h("label", { className: "block space-y-1 text-sm" },
          h("span", null, "Proposed task"),
          h("input", { className: "min-h-11 w-full rounded-md border bg-background px-3", value: proposalTitle, onChange: function (event) { setProposalTitle(event.target.value); }, "data-testid": "coordinator-proposal-title" }),
        ),
        h("label", { className: "block space-y-1 text-sm" },
          h("span", null, "Instructions"),
          h("textarea", { className: "min-h-20 w-full rounded-md border bg-background p-3", value: proposalDescription, onChange: function (event) { setProposalDescription(event.target.value); }, "data-testid": "coordinator-proposal-description" }),
        ),
        h(ui.Button, { type: "button", variant: "outline", className: "min-h-11 w-full sm:w-auto", disabled: !proposalTitle.trim() || snapshot.state === "paused", onClick: createProposal, "data-testid": "coordinator-proposal-submit" }, "Save proposal"),
        proposals.length ? h("div", { className: "space-y-2", "data-testid": "coordinator-proposal-list" }, proposals.map(function (proposal) {
          var input = {};
          try { input = JSON.parse(proposal.inputJson || "{}"); } catch (_error) {}
          return h("article", { key: proposal.proposalId + ":" + proposal.revision, className: "space-y-2 rounded-md border p-3", "data-proposal-id": proposal.proposalId },
            h("h3", { className: "font-medium" }, input.title || "Untitled proposal"),
            h("p", { className: "text-sm text-muted-foreground" }, input.description || "No instructions provided."),
            h("p", { className: "text-xs text-muted-foreground" }, "Revision " + proposal.revision + " · " + proposal.approvalState + (proposal.taskId ? " · Task " + proposal.taskId : "")),
            proposal.approvalState === "pending" ? h("div", { className: "flex flex-wrap gap-2" },
              h(ui.Button, { type: "button", className: "min-h-11", onClick: function () { decideProposal(proposal, "approve"); }, "data-testid": "coordinator-proposal-approve" }, "Approve"),
              h(ui.Button, { type: "button", variant: "outline", className: "min-h-11", onClick: function () { decideProposal(proposal, "reject"); }, "data-testid": "coordinator-proposal-reject" }, "Reject"),
            ) : null,
            proposal.approvalState === "approved" && !proposal.taskId ? h(ui.Button, { type: "button", variant: "outline", className: "min-h-11", onClick: function () { reconcileProposal(proposal); }, "data-testid": "coordinator-proposal-reconcile" }, "Reconcile task receipt") : null,
          );
        })) : h("p", { className: "text-sm text-muted-foreground" }, "No proposals yet."),
      );

      var tasksPanel = h("div", { className: "space-y-4", "data-testid": "coordinator-tasks-panel" },
        policy ? h("section", { className: "rounded-md border p-3 text-sm", "data-testid": "coordinator-policy-status" },
          h("p", { className: "font-medium" }, "Policy limits"),
          h("p", { className: "text-muted-foreground" }, policy.activeRuns + " of " + policy.maxConcurrentRuns + " concurrent runs · " + (policy.usageState || "unknown") + " usage"),
          policy.estimatedBudgetUsd > 0 ? h("p", { className: "text-muted-foreground" }, "Budget: USD " + Number(policy.usageUsd || 0).toFixed(2) + " / USD " + Number(policy.estimatedBudgetUsd).toFixed(2) + (policy.potentialOvershoot ? " · active work may add cost" : "")) : null,
          policy.blocked ? h("p", { role: "status", className: "text-amber-700" }, "New coordinator work is held: " + policy.reason) : null,
        ) : null,
        h("section", { className: "space-y-2 rounded-md border p-3", "data-testid": "coordinator-adopt-workspace-task" },
          h("h2", { className: "font-semibold" }, "Adopt a workspace task"),
          h("p", { className: "text-sm text-muted-foreground" }, "Enter a task ID to request an explicit management claim. The Host checks current ownership and version."),
          h("label", { className: "block space-y-1 text-sm" },
            h("span", null, "Workspace task ID"),
            h("input", { className: "min-h-11 w-full rounded-md border bg-background px-3", value: externalTaskId, onChange: function (event) { setExternalTaskId(event.target.value); }, "data-testid": "coordinator-external-task-id" }),
          ),
          h(ui.Button, { type: "button", variant: "outline", className: "min-h-11 w-full sm:w-auto", disabled: !externalTaskId.trim() || snapshot.state === "paused", onClick: adoptExternalTask, "data-testid": "coordinator-adopt-external-task" }, "Adopt task by ID"),
        ),
        h("section", { className: "space-y-2 rounded-md border p-3" },
          h("h2", { className: "font-semibold" }, "Delegate a task"),
          h("label", { className: "block space-y-1 text-sm" },
            h("span", null, "Task title"),
            h("input", { className: "min-h-11 w-full rounded-md border bg-background px-3", value: taskTitle, onChange: function (event) { setTaskTitle(event.target.value); }, "data-testid": "coordinator-delegate-title" }),
          ),
          h("label", { className: "block space-y-1 text-sm" },
            h("span", null, "Instructions"),
            h("textarea", { className: "min-h-20 w-full rounded-md border bg-background p-3", value: taskDescription, onChange: function (event) { setTaskDescription(event.target.value); }, "data-testid": "coordinator-delegate-description" }),
          ),
          h(ui.Button, { type: "button", className: "min-h-11 w-full sm:w-auto", disabled: !taskTitle.trim() || snapshot.state === "paused", onClick: delegateTask, "data-testid": "coordinator-delegate-submit" }, "Delegate task"),
        ),
        proposalPanel,
        taskList(),
      );

      var outcomesPanel = taskOutcomes();

      if (!workspaceId) {
        return h("div", { className: "flex h-full items-center justify-center p-6", "data-testid": "coordinator-page" },
          h("p", { role: "status" }, "Select a workspace to open its coordinator instances."),
        );
      }

      if (!instances.length) {
        return h("div", { className: "flex h-[calc(100dvh-3.5rem)] min-h-0 items-center justify-center p-4", "data-testid": "coordinator-page" },
          h("section", { className: "w-full max-w-lg rounded-lg border p-6 text-center", "data-testid": "coordinator-empty" },
            h("h1", { className: "text-xl font-semibold" }, "Create a coordinator instance"),
            h("p", { className: "my-3 text-sm text-muted-foreground" }, "Choose a role and profile. Each instance keeps its own instructions, conversation, task links, and memory."),
            h(ui.Button, { type: "button", className: "min-h-11", onClick: function () { host.navigate(SETTINGS_PATH + "?new=1"); }, "data-testid": "coordinator-add-instance" }, "Add instance"),
          ),
        );
      }

      var selected = instances.find(function (item) { return item.key === selectedKey; }) || instances[0];
      var pickerOptions = instances.map(function (item) { return { key: item.key, label: item.name }; });
      var headerActions = h("div", { className: "flex flex-wrap items-center gap-2" },
        h(ui.Button, { type: "button", variant: "outline", className: "min-h-11", onClick: function () { host.navigate(SETTINGS_PATH + "?instanceKey=" + encodeURIComponent(selectedKey)); }, "data-testid": "coordinator-open-settings" }, "Settings"),
        h(ui.Button, { type: "button", variant: "outline", className: "min-h-11", onClick: function () { host.navigate(SETTINGS_PATH + "?new=1"); }, "data-testid": "coordinator-add-instance" }, "Add instance"),
      );

      return h("div", { className: "flex h-[calc(100dvh-3.5rem)] min-h-0 min-w-0 flex-col overflow-hidden", "data-testid": "coordinator-page" },
        h("header", { className: "flex shrink-0 flex-wrap items-center gap-2 border-b bg-background p-3" },
          h("div", { className: "min-w-0 flex-1" },
            h("h1", { className: "truncate font-semibold" }, selected.name),
            h("p", { className: "truncate text-xs text-muted-foreground" }, selected.role + " · " + selected.agent_profile_id),
          ),
          headerActions,
        ),
        error ? h("p", { role: "alert", className: "shrink-0 px-3 py-2 text-sm text-destructive" }, error) : null,
        h("div", { className: "min-h-0 flex-1" }, h(ui.WorkspaceAgentChat, {
          conversation: snapshot,
          controller: controller,
          instances: pickerOptions,
          onSelectInstance: selectInstance,
          tasksPanel: tasksPanel,
          outcomesPanel: outcomesPanel,
          onStatus: function (next) { if (selectedRef.current === next.instanceKey) setSnapshot(next); },
        })),
      );
    }

    return ConversationPage;
  }

  function makeSettingsPage(host) {
    var React = host.React;
    var h = host.jsx;
    var ui = host.ui;

    return function SettingsPage() {
      var workspaceState = React.useState(host.context.getActiveWorkspaceId() || "");
      var workspaceId = workspaceState[0];
      var setWorkspaceId = workspaceState[1];
      var catalogState = React.useState({ agentProfiles: [], executorProfiles: [], roles: [], defaultAgentProfileId: "", defaultExecutorId: "" });
      var catalog = catalogState[0];
      var setCatalog = catalogState[1];
      var loadedState = React.useState(false);
      var loaded = loadedState[0];
      var setLoaded = loadedState[1];
      var errorState = React.useState("");
      var error = errorState[0];
      var setError = errorState[1];
      var search = new URLSearchParams(window.location.search);
      var creating = search.get("new") === "1";
      var instanceKey = search.get("instanceKey") || "";
      var initial = React.useState({
        key: "",
        name: "",
        role: "chief-of-staff",
        agent_profile_id: "",
        executor_id: "",
        executor_profile_id: "",
        instructions: "Coordinate work through Kandev task and conversation APIs. Report blockers with evidence.",
        task_scope: "workspace",
        estimated_budget_usd: 10,
        max_concurrent_runs: 2,
        revision: 0,
        conversation_revision: 0,
      });
      var form = initial[0];
      var setForm = initial[1];
      var scheduleState = React.useState([]);
      var schedules = scheduleState[0];
      var setSchedules = scheduleState[1];
      var routineNameState = React.useState("");
      var routineName = routineNameState[0];
      var setRoutineName = routineNameState[1];
      var routinePromptState = React.useState("");
      var routinePrompt = routinePromptState[0];
      var setRoutinePrompt = routinePromptState[1];
      var routineCronState = React.useState("0 9 * * MON-FRI");
      var routineCron = routineCronState[0];
      var setRoutineCron = routineCronState[1];
      var adminState = React.useState(null);
      var adminCatalog = adminState[0];
      var setAdminCatalog = adminState[1];
      var adminNameState = React.useState("");
      var adminName = adminNameState[0];
      var setAdminName = adminNameState[1];
      var adminDescriptionState = React.useState("");
      var adminDescription = adminDescriptionState[0];
      var setAdminDescription = adminDescriptionState[1];
      var adminPromptState = React.useState("");
      var adminPrompt = adminPromptState[0];
      var setAdminPrompt = adminPromptState[1];

      function patchForm(field, value) {
        setForm(Object.assign({}, form, (function () { var patch = {}; patch[field] = value; return patch; })()));
      }

      React.useEffect(function () {
        var unsubscribe = host.context.subscribeActiveWorkspace(function (next) { setWorkspaceId(next || ""); });
        return unsubscribe;
      }, []);

      React.useEffect(function () {
        if (!workspaceId) return;
        Promise.all([
          action(host, workspaceId, "instance.catalog"),
          action(host, workspaceId, "instance.list"),
        ]).then(function (values) {
          var nextCatalog = values[0];
          var instances = values[1].instances || [];
          setCatalog(nextCatalog);
          if (!creating) {
            var existing = instances.find(function (item) { return item.key === instanceKey; });
            if (!existing) throw new Error("This coordinator instance is not available in the active workspace");
            setForm(Object.assign({}, existing));
            return action(host, workspaceId, "routine.list", { instance_key: existing.key }).then(function (response) {
              setSchedules(response.schedules || []);
            }).catch(function () { setSchedules([]); }).then(function () { setLoaded(true); });
          } else {
            setForm(Object.assign({}, form, {
              agent_profile_id: nextCatalog.defaultAgentProfileId || (nextCatalog.agentProfiles[0] && nextCatalog.agentProfiles[0].id) || "",
              executor_id: nextCatalog.defaultExecutorId || "",
              executor_profile_id: (nextCatalog.executorProfiles[0] && nextCatalog.executorProfiles[0].id) || "",
            }));
          }
          setLoaded(true);
        }).catch(function (reason) {
          setError(reason.message || "Could not load coordinator settings");
          setLoaded(true);
        });
      }, [workspaceId]);

      function save() {
        var slug = form.name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 48);
        var key = form.key || slug;
        if (!key) {
          setError("Enter an instance name before saving.");
          return;
        }
        setError("");
        action(host, workspaceId, "instance.save", {
          instance_key: key,
          name: form.name,
          role: form.role,
          agent_profile_id: form.agent_profile_id,
          executor_id: form.executor_id,
          executor_profile_id: form.executor_profile_id,
          instructions: form.instructions,
          task_scope: form.task_scope,
          estimated_budget_usd: Number(form.estimated_budget_usd) || 0,
          max_concurrent_runs: Number(form.max_concurrent_runs) || 0,
          expected_revision: form.revision || 0,
        }).then(function (response) {
          setForm(Object.assign({}, form, response.instance));
          host.navigate(HOME_PATH);
        }).catch(function (reason) {
          setError(reason.message || "Could not save coordinator instance");
        });
      }

      function pause() {
        action(host, workspaceId, "instance.pause", {
          instance_key: form.key,
          expected_revision: form.conversation_revision,
          paused: !form.paused,
        }).then(accepted).then(function () {
          host.navigate(HOME_PATH);
        }).catch(function (reason) {
          setError(reason.message || "Could not change pause state");
        });
      }

      function createRoutine() {
        if (!form.key || !routineName.trim() || !routinePrompt.trim() || !routineCron.trim()) return;
        action(host, workspaceId, "routine.create", {
          instance_key: form.key,
          request_id: host.utils.generateUUID(),
          name: routineName.trim(),
          prompt: routinePrompt.trim(),
          triggers: [{ type: "scheduled", enabled: true, config: { cron_expression: routineCron.trim(), timezone: "UTC" } }],
        }).then(function () {
          setRoutineName("");
          setRoutinePrompt("");
          return action(host, workspaceId, "routine.list", { instance_key: form.key });
        }).then(function (response) { setSchedules(response.schedules || []); }).catch(function (reason) {
          setError(reason.message || "Could not create the routine");
        });
      }

      function toggleRoutine(schedule) {
        action(host, workspaceId, "routine.enable", {
          instance_key: form.key,
          schedule_id: schedule.id,
          request_id: host.utils.generateUUID(),
          expected_resource_revision: schedule.resourceRevision,
          enabled: !schedule.enabled,
        }).then(function () {
          return action(host, workspaceId, "routine.list", { instance_key: form.key });
        }).then(function (response) { setSchedules(response.schedules || []); }).catch(function (reason) {
          setError(reason.message || "Could not update the routine");
        });
      }

      function deleteRoutine(schedule) {
        action(host, workspaceId, "routine.delete", {
          instance_key: form.key,
          schedule_id: schedule.id,
          request_id: host.utils.generateUUID(),
          expected_resource_revision: schedule.resourceRevision,
        }).then(function () {
          return action(host, workspaceId, "routine.list", { instance_key: form.key });
        }).then(function (response) { setSchedules(response.schedules || []); }).catch(function (reason) {
          setError(reason.message || "Could not delete the routine");
        });
      }

      function loadWorkspaceAdmin() {
        return action(host, workspaceId, "workspace.admin.catalog").then(function (response) {
          setAdminCatalog(response);
          return response;
        }).catch(function (reason) {
          setError(reason.message || "Workspace administration is unavailable");
          return null;
        });
      }

      function createWorkspaceWorkflow() {
        if (!adminCatalog || !adminCatalog.workspace || !adminName.trim() || !adminCatalog.administration.authorized) return;
        action(host, workspaceId, "workspace.admin.apply", {
          operation: "workflow.create",
          request_id: host.utils.generateUUID(),
          expected_workspace_resource_version: adminCatalog.workspace.resourceVersion,
          name: adminName.trim(),
          description: adminDescription,
          prompt: adminPrompt,
        }).then(function (response) {
          accepted(response);
          setAdminName("");
          setAdminDescription("");
          setAdminPrompt("");
          return loadWorkspaceAdmin();
        }).catch(function (reason) { setError(reason.message || "Could not create the workspace workflow"); });
      }

      function field(label, value, onChange, id, type) {
        return h("label", { className: "block space-y-1 text-sm", htmlFor: id },
          h("span", { className: "font-medium" }, label),
          h("input", { id: id, type: type || "text", className: "min-h-11 w-full rounded-md border bg-background px-3", value: value == null ? "" : value, onChange: onChange, "data-testid": "coordinator-field-" + id }),
        );
      }

      if (!loaded) return h("div", { className: "p-4", "data-testid": "coordinator-settings" }, "Loading coordinator settings…");

      return h("main", { className: "mx-auto flex h-[calc(100dvh-3.5rem)] min-h-0 w-full max-w-3xl flex-col overflow-y-auto p-4 sm:p-6", "data-testid": "coordinator-settings" },
        h("header", { className: "mb-4 flex shrink-0 items-center gap-3" },
          h(ui.Button, { type: "button", variant: "outline", className: "min-h-11", onClick: function () { host.navigate(HOME_PATH); }, "aria-label": "Back to coordinator" }, "Back"),
          h("div", { className: "min-w-0 flex-1" },
            h("h1", { className: "text-xl font-semibold" }, creating ? "Add coordinator instance" : form.name),
            h("p", { className: "text-sm text-muted-foreground" }, "Each instance has its own profile, instructions, retained conversation, tasks, and memory."),
          ),
          h(ui.Button, { type: "button", className: "min-h-11", onClick: save, disabled: !form.name || !form.role || !form.agent_profile_id, "data-testid": "coordinator-save" }, "Save"),
        ),
        error ? h("p", { role: "alert", className: "mb-3 text-sm text-destructive" }, error) : null,
        h("div", { className: "grid min-w-0 gap-4 md:grid-cols-2" },
          field("Instance name", form.name, function (event) { patchForm("name", event.target.value); }, "name"),
          h("label", { className: "block space-y-1 text-sm", htmlFor: "role" },
            h("span", { className: "font-medium" }, "Role"),
            h("select", { id: "role", className: "min-h-11 w-full rounded-md border bg-background px-3", value: form.role, onChange: function (event) { patchForm("role", event.target.value); }, "data-testid": "coordinator-field-role" },
              catalog.roles.map(function (role) { return h("option", { key: role.key, value: role.key }, role.displayName); }),
            ),
          ),
          h("label", { className: "block space-y-1 text-sm", htmlFor: "agent-profile" },
            h("span", { className: "font-medium" }, "Agent profile"),
            h("select", { id: "agent-profile", className: "min-h-11 w-full rounded-md border bg-background px-3", value: form.agent_profile_id, onChange: function (event) { patchForm("agent_profile_id", event.target.value); }, "data-testid": "coordinator-field-agent-profile" },
              [h("option", { key: "", value: "" }, "Choose a profile")].concat(catalog.agentProfiles.map(function (profile) {
                return h("option", { key: profile.id, value: profile.id }, profile.displayName || profile.name || profile.id);
              })),
            ),
          ),
          h("label", { className: "block space-y-1 text-sm", htmlFor: "executor-profile" },
            h("span", { className: "font-medium" }, "Executor profile"),
            h("select", { id: "executor-profile", className: "min-h-11 w-full rounded-md border bg-background px-3", value: form.executor_profile_id || "", onChange: function (event) { patchForm("executor_profile_id", event.target.value); }, "data-testid": "coordinator-field-executor-profile" },
              [h("option", { key: "", value: "" }, "Use workspace default")].concat(catalog.executorProfiles.map(function (profile) {
                return h("option", { key: profile.id, value: profile.id }, profile.displayName || profile.id);
              })),
            ),
          ),
          h("label", { className: "block space-y-1 text-sm", htmlFor: "task-scope" },
            h("span", { className: "font-medium" }, "Task scope"),
            h("select", { id: "task-scope", className: "min-h-11 w-full rounded-md border bg-background px-3", value: form.task_scope || "workspace", onChange: function (event) { patchForm("task_scope", event.target.value); }, "data-testid": "coordinator-field-task-scope" },
              h("option", { value: "workspace" }, "Workspace tasks"),
              h("option", { value: "selected" }, "Selected tasks"),
            ),
          ),
          field("Estimated budget (USD)", form.estimated_budget_usd, function (event) { patchForm("estimated_budget_usd", event.target.value); }, "budget", "number"),
          field("Maximum concurrent runs", form.max_concurrent_runs, function (event) { patchForm("max_concurrent_runs", event.target.value); }, "max-concurrent-runs", "number"),
        ),
        h("label", { className: "mt-4 block space-y-1 text-sm", htmlFor: "instructions" },
          h("span", { className: "font-medium" }, "Instructions"),
          h("textarea", { id: "instructions", className: "min-h-36 w-full rounded-md border bg-background p-3", value: form.instructions, onChange: function (event) { patchForm("instructions", event.target.value); }, "data-testid": "coordinator-field-instructions" }),
        ),
        !creating ? h("section", { className: "mt-5 space-y-3 border-t pt-4", "data-testid": "coordinator-routines" },
          h("h2", { className: "font-semibold" }, "Recurring routines"),
          h("p", { className: "text-sm text-muted-foreground" }, "Schedules use the Host automation service and target this retained coordinator conversation."),
          h("label", { className: "block space-y-1 text-sm" },
            h("span", null, "Routine name"),
            h("input", { className: "min-h-11 w-full rounded-md border bg-background px-3", value: routineName, onChange: function (event) { setRoutineName(event.target.value); }, "data-testid": "coordinator-routine-name" }),
          ),
          h("label", { className: "block space-y-1 text-sm" },
            h("span", null, "Cron schedule (UTC)"),
            h("input", { className: "min-h-11 w-full rounded-md border bg-background px-3", value: routineCron, onChange: function (event) { setRoutineCron(event.target.value); }, "data-testid": "coordinator-routine-cron" }),
          ),
          h("label", { className: "block space-y-1 text-sm" },
            h("span", null, "Prompt"),
            h("textarea", { className: "min-h-20 w-full rounded-md border bg-background p-3", value: routinePrompt, onChange: function (event) { setRoutinePrompt(event.target.value); }, "data-testid": "coordinator-routine-prompt" }),
          ),
          h(ui.Button, { type: "button", variant: "outline", className: "min-h-11 w-full sm:w-auto", disabled: !routineName.trim() || !routinePrompt.trim(), onClick: createRoutine, "data-testid": "coordinator-routine-create" }, "Create routine"),
          schedules.length ? h("div", { className: "space-y-2", "data-testid": "coordinator-routine-list" }, schedules.map(function (schedule) {
            return h("article", { key: schedule.id, className: "flex flex-wrap items-center gap-2 rounded-md border p-3" },
              h("div", { className: "min-w-0 flex-1" },
                h("p", { className: "font-medium" }, schedule.name),
                h("p", { className: "text-xs text-muted-foreground" }, schedule.enabled ? "Enabled" : "Paused"),
              ),
              h(ui.Button, { type: "button", variant: "outline", className: "min-h-11", onClick: function () { toggleRoutine(schedule); } }, schedule.enabled ? "Pause routine" : "Resume routine"),
              h(ui.Button, { type: "button", variant: "outline", className: "min-h-11", onClick: function () { deleteRoutine(schedule); } }, "Delete routine"),
            );
          })) : h("p", { className: "text-sm text-muted-foreground" }, "No recurring routines."),
        ) : null,
        !creating ? h("section", { className: "mt-5 space-y-3 border-t pt-4", "data-testid": "coordinator-workspace-admin" },
          h("div", { className: "flex flex-wrap items-center justify-between gap-2" },
            h("h2", { className: "font-semibold" }, "Optional workspace administration"),
            h(ui.Button, { type: "button", variant: "outline", className: "min-h-11", onClick: loadWorkspaceAdmin, "data-testid": "coordinator-admin-load" }, "Load workflow catalog"),
          ),
          h("p", { className: "text-sm text-muted-foreground" }, "Workflow creation uses a separate Host capability and the current workspace version. Scripts, secrets, and destructive changes stay in native settings."),
          adminCatalog ? h("div", { className: "space-y-3", "data-testid": "coordinator-admin-catalog" },
            h("p", { className: "text-sm" }, adminCatalog.workspace.name + " · revision " + adminCatalog.workspace.resourceVersion),
            adminCatalog.administration.create.authorized ? null : h("p", { role: "status", className: "text-sm text-amber-700" }, adminCatalog.administration.create.reason || "Grant workflow administration in this plugin's workspace capabilities."),
            adminCatalog.workflows.length ? h("ul", { className: "space-y-1 text-sm" }, adminCatalog.workflows.map(function (workflow) {
              return h("li", { key: workflow.id }, workflow.name + " · revision " + workflow.resourceVersion);
            })) : h("p", { className: "text-sm text-muted-foreground" }, "No workflows are available."),
            h("h3", { className: "pt-2 font-medium" }, "Create a workflow"),
            field("Workflow name", adminName, function (event) { setAdminName(event.target.value); }, "admin-workflow-name"),
            field("Description", adminDescription, function (event) { setAdminDescription(event.target.value); }, "admin-workflow-description"),
            h("label", { className: "block space-y-1 text-sm" },
              h("span", { className: "font-medium" }, "Workflow instructions"),
              h("textarea", { className: "min-h-28 w-full rounded-md border bg-background p-3", value: adminPrompt, onChange: function (event) { setAdminPrompt(event.target.value); }, "data-testid": "coordinator-admin-workflow-prompt" }),
            ),
            h(ui.Button, { type: "button", variant: "outline", className: "min-h-11 w-full sm:w-auto", disabled: !adminCatalog.administration.create.authorized || !adminName.trim(), onClick: createWorkspaceWorkflow, "data-testid": "coordinator-admin-workflow-create" }, "Create workspace workflow"),
          ) : null,
        ) : null,
        !creating ? h("section", { className: "mt-5 flex flex-wrap items-center justify-between gap-3 border-t pt-4" },
          h("p", { className: "text-sm text-muted-foreground" }, form.paused ? "Paused. Memory and pending inputs are retained." : "Pausing stops new coordinator turns and preserves pending inputs."),
          h(ui.Button, { type: "button", variant: "outline", className: "min-h-11", onClick: pause, "data-testid": "coordinator-settings-pause" }, form.paused ? "Resume instance" : "Pause instance"),
        ) : null,
      );
    };
  }

  window.registerKandevPlugin(PLUGIN_ID, {
    initialize: function (registry, host) {
      registry.registerNavItem({ id: "coordinator", label: "Coordinator", path: HOME_PATH, icon: "workflow", section: "main" });
      registry.registerRoute(HOME_PATH, makeConversationPage(host));
      registry.registerRoute(SETTINGS_PATH, makeSettingsPage(host), { topbar: { title: "Coordinator settings", backHref: HOME_PATH } });
    },
    destroy: function () {},
  });
})();
