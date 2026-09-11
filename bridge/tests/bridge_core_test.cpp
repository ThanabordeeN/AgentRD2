#include <cassert>
#include <cmath>
#include <iostream>
#include <string>
#include <vector>

#include "action_executor.hpp"
#include "mini_json.hpp"
#include "native_hashes.hpp"
#include "ped_scanner.hpp"
#include "story_gate.hpp"
#include "mock_native_api.hpp"

using namespace rdr2ai;
using rdr2ai::test::MockNativeApi;

static ActionResult run(ActionExecutor& executor, const std::string& tool, const std::string& json) {
    ActionRequest request;
    request.npc_id = "ped_2";
    request.tool = tool;
    request.request_id = "act_test";
    request.arguments_json = json;
    ActionResult result;
    executor.execute(request, result);
    return result;
}

int main() {
    MockNativeApi api;
    api.destinations["Valentine Saloon"] = {100.0f, 200.0f, 10.0f};
    ActionExecutor executor(&api);

    struct Case {
        const char* tool;
        const char* args;
        std::uint64_t expected_hash;
    };
    const std::vector<Case> cases = {
        {"go_to", R"({"destination":"Valentine Saloon"})", hash::TASK_GO_TO_COORD_ANY_MEANS},
        {"wander", R"({"radius":8})", hash::TASK_WANDER_IN_AREA},
        {"follow", R"({"entity":"player"})", hash::TASK_FOLLOW_TO_OFFSET_OF_ENTITY},
        {"stop", "{}", hash::CLEAR_PED_TASKS},
        {"wait", R"({"duration":2})", hash::TASK_PAUSE},
        {"look_at", R"({"entity":"player"})", hash::TASK_LOOK_AT_ENTITY},
        {"face", R"({"entity":"player"})", hash::TASK_TURN_PED_TO_FACE_ENTITY},
        {"clear_attention", "{}", hash::TASK_CLEAR_LOOK_AT},
        {"say", R"({"text":"Evening."})", hash::PLAY_PED_AMBIENT_SPEECH_NATIVE},
        {"gesture", R"({"type":"nod"})", hash::TASK_PLAY_ANIM},
        {"think", R"({"style":"listen"})", hash::TASK_PLAY_ANIM},
        {"investigate", R"({"position":[1,2,3]})", hash::TASK_GO_TO_COORD_ANY_MEANS},
        {"flee_from", R"({"entity":"player"})", hash::TASK_FLEE_PED},
        {"walk_away", R"({"entity":"player"})", hash::TASK_WALK_AWAY},
        {"react", R"({"entity":"player","reaction":"DEFAULT"})", hash::TASK_REACT},
        {"hands_up", "{}", hash::TASK_HANDS_UP},
        {"cower", "{}", hash::TASK_COWER},
        {"duck", "{}", hash::TASK_DUCK},
        {"jump", "{}", hash::TASK_JUMP},
        {"mount", R"({"entity":"horse"})", hash::TASK_MOUNT_ANIMAL},
        {"dismount", "{}", hash::TASK_DISMOUNT_ANIMAL},
        {"item_interaction", R"({"item":"P_BOOK01X","interaction":"PROP_HUMAN_BOOK_TABLE"})", hash::START_TASK_ITEM_INTERACTION},
        {"animal_interaction", R"({"target":"target","interaction_type":"PET","interaction_model":"DOG"})", hash::TASK_ANIMAL_INTERACTION},
        {"horse_action", R"({"action":1})", hash::TASK_HORSE_ACTION},
        {"aim_at", R"({"entity":"player"})", hash::TASK_AIM_GUN_AT_ENTITY},
        {"shoot_at", R"({"entity":"player"})", hash::TASK_SHOOT_AT_ENTITY},
        {"attack", R"({"entity":"player"})", hash::TASK_COMBAT_PED_TIMED},
        {"take_cover", R"({"from_entity":"player"})", hash::TASK_SEEK_COVER_FROM_PED},
    };

    for (const auto& item : cases) {
        api.calls.clear();
        ActionResult result = run(executor, item.tool, item.args);
        if (result.status != "started") {
            std::cerr << "action failed: " << item.tool << " reason=" << result.reason << "\n";
            return 1;
        }
        bool found = false;
        for (const auto& call : api.calls) {
            if (call.hash == item.expected_hash) found = true;
        }
        assert(found && "expected native hash not invoked");
    }

    // Story gate must reject story/mission/scripted/cutscene peds.
    StoryGate gate;
    PedSnapshot safe;
    safe.is_ped = true; safe.is_human = true; safe.is_alive = true;
    assert(gate.can_activate(safe));
    PedSnapshot mission = safe;
    mission.is_mission_owned = true;
    assert(!gate.can_activate(mission));

    // Ped scanner JSON contains the metadata needed by the runtime.
    PedSnapshotData ped;
    ped.entity_id = "ped_7";
    ped.handle = 7;
    ped.is_ped = true; ped.is_human = true; ped.is_alive = true;
    ped.distance_m = 6.5f;
    ped.quest_dialogue = true;
    ped.quest_id = "quest_valentine_livestock";
    PedScanner scanner(&api, 20.0f);
    std::string scan_json = scanner.to_scan_json({ped});
    Json parsed = Json::parse(scan_json);
    assert(parsed["type"].as_string() == "ped_scan");
    assert(parsed["peds"].size() == 1);
    assert(parsed["peds"][0]["metadata"]["quest_dialogue"].as_bool());
    assert(parsed["peds"][0]["metadata"]["quest_id"].as_string() == "quest_valentine_livestock");

    std::cout << "bridge_core_test: " << cases.size() << " actions mapped, scanner/story gate OK\n";
    return 0;
}
