#include "bridge_runtime.hpp"

#include <algorithm>
#include <cmath>

#include "mini_json.hpp"

namespace rdr2ai {
namespace {

int estimated_completion_ms(const ActionRequest& request, const ActionResult& result) {
    (void)result;
    if (request.tool == "wait" || request.tool == "pause") {
        try {
            Json args = Json::parse(request.arguments_json.empty() ? "{}" : request.arguments_json);
            double duration = args.contains("duration") ? args["duration"].as_number(2.0) : 2.0;
            return static_cast<int>(duration * 1000.0) + 250;
        } catch (...) {
            return 2250;
        }
    }
    if (request.tool == "say") {
        try {
            Json args = Json::parse(request.arguments_json.empty() ? "{}" : request.arguments_json);
            std::string text = args.contains("text") ? args["text"].as_string() : "";
            return 500 + static_cast<int>(text.size()) * 55;
        } catch (...) {
            return 2000;
        }
    }
    if (request.tool == "look_at" || request.tool == "face" || request.tool == "clear_attention") {
        return 800;
    }
    if (request.tool == "gesture" || request.tool == "think") {
        return 2800;
    }
    if (request.tool == "wander") return 5000;
    if (request.tool == "go_to" || request.tool == "follow" || request.tool == "investigate" ||
        request.tool == "flee_from" || request.tool == "walk_away") {
        return 8000;
    }
    return 4000;
}

}  // namespace

BridgeRuntime::BridgeRuntime(INativeApi* native_api, std::string host, unsigned short port,
                             int scan_interval_ms)
    : api_(native_api),
      scanner_(native_api),
      action_executor_(native_api),
      ipc_(std::move(host), port),
      scan_interval_ms_(scan_interval_ms),
      last_scan_(std::chrono::steady_clock::now()) {}

BridgeRuntime::~BridgeRuntime() {
    stop();
}

bool BridgeRuntime::start() {
    return ipc_.start([this](const std::string& line) { onIpcMessage(line); });
}

void BridgeRuntime::stop() {
    ipc_.stop();
}

void BridgeRuntime::tick() {
    const auto now = std::chrono::steady_clock::now();

    // 1. Send periodic scans.
    if (std::chrono::duration_cast<std::chrono::milliseconds>(now - last_scan_).count() >= scan_interval_ms_) {
        if (api_ != nullptr) {
            auto peds = scanner_.scan_nearby_peds();
            std::vector<PedSnapshotData> safe;
            safe.reserve(peds.size());
            for (const auto& ped : peds) {
                if (story_gate_.can_activate(ped)) safe.push_back(ped);
            }
            ipc_.send_ped_scan(scanner_.to_scan_json(safe));
        }
        last_scan_ = now;
    }

    // 2. Emit completions for actions that have plausibly finished.
    for (auto it = pending_.begin(); it != pending_.end();) {
        if (now >= it->due) {
            it->result.status = "completed";
            ipc_.send_action_result(it->result);
            it = pending_.erase(it);
        } else {
            ++it;
        }
    }
}

void BridgeRuntime::onPushToTalk(bool down) {
    Json message = Json::object();
    message["type"] = Json("push_to_talk");
    message["action"] = Json(down ? "start" : "stop");
    ipc_.send_json(message);
}

void BridgeRuntime::onIpcMessage(const std::string& line) {
    Json message;
    try {
        message = Json::parse(line);
    } catch (const std::exception&) {
        return;
    }
    std::string type = message.contains("type") ? message["type"].as_string() : "";
    if (type != "action_request") return;

    const Json& request_json = message["request"];
    if (!request_json.is_object()) return;

    ActionRequest request;
    request.npc_id = message.contains("npc_id") ? message["npc_id"].as_string() : "";
    request.tool = request_json.contains("tool") ? request_json["tool"].as_string() : "";
    request.request_id = request_json.contains("request_id") ? request_json["request_id"].as_string() : "";
    request.arguments_json = request_json.contains("arguments") ? request_json["arguments"].dump() : "{}";

    ActionResult result;
    if (!action_executor_.execute(request, result)) {
        ipc_.send_action_result(result);
        return;
    }
    if (result.status == "started") {
        queueCompletion(request, result);
    } else if (result.status == "failed") {
        ipc_.send_action_result(result);
    }
}

void BridgeRuntime::queueCompletion(const ActionRequest& request, ActionResult result) {
    int duration = estimated_completion_ms(request, result);
    pending_.push_back({
        std::move(result),
        std::chrono::steady_clock::now() + std::chrono::milliseconds(duration),
    });
}

void BridgeRuntime::sendActionFailures() {
    // Reserved for future action state machines.
}

}  // namespace rdr2ai
