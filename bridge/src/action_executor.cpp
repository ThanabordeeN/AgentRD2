#include "action_executor.hpp"

#include <cmath>
#include <cstring>
#include <string>
#include <unordered_map>
#include <utility>
#include <vector>

#include "mini_json.hpp"
#include "native_hashes.hpp"

namespace rdr2ai {
namespace {

using hash::ANIM_FLAG_ABORT_ON_PED_MOVEMENT;
using hash::ANIM_FLAG_GESTURE;
using hash::ANIM_FLAG_LOOPING;
using hash::ANIM_FLAG_NOT_INTERRUPTABLE;
using hash::ANIM_FLAG_SECONDARY;
using hash::ANIM_FLAG_UPPERBODY;

std::uint32_t joaat(const std::string& value) {
    std::uint32_t result = 0;
    for (unsigned char c : value) {
        result += c;
        result += (result << 10);
        result ^= (result >> 6);
    }
    result += (result << 3);
    result ^= (result >> 11);
    result += (result << 15);
    return result;
}

bool get_position(const Json& value, Vec3& out) {
    if (value.is_array()) {
        const auto& items = value.items();
        if (items.size() < 3) return false;
        out.x = items[0].as_float();
        out.y = items[1].as_float();
        out.z = items[2].as_float();
        return true;
    }
    if (value.is_object()) {
        if (value.contains("position")) return get_position(value["position"], out);
    }
    return false;
}

bool get_destination(INativeApi* api, const Json& args, Vec3& out) {
    if (args.contains("destination")) {
        const Json& destination = args["destination"];
        if (get_position(destination, out)) return true;
        if (destination.is_string() && api->resolveDestination(destination.as_string(), out)) {
            return true;
        }
    }
    return get_position(args, out);
}

std::uint32_t resolve_entity_arg(INativeApi* api, const Json& args, const char* key) {
    if (!args.is_object() || !args.contains(key)) return 0;
    const Json& value = args[key];
    if (value.is_number()) return static_cast<std::uint32_t>(value.as_int());
    if (value.is_string()) return api->resolveEntity(value.as_string());
    return 0;
}

std::uint32_t request_ped(const ActionRequest& request, INativeApi* api) {
    if (request.npc_id.empty()) return 0;
    std::uint32_t handle = api->resolveEntity(request.npc_id);
    if (handle != 0) return handle;
    std::string numeric = request.npc_id;
    if (numeric.rfind("ped_", 0) == 0) numeric = numeric.substr(4);
    try {
        std::size_t consumed = 0;
        unsigned long parsed = std::stoul(numeric, &consumed, 0);
        if (consumed == numeric.size()) return static_cast<std::uint32_t>(parsed);
    } catch (...) {
    }
    return 0;
}

bool resolve_entity_or_position(INativeApi* api, const Json& args, std::uint32_t& entity, Vec3& position,
                                bool& has_position) {
    entity = resolve_entity_arg(api, args, "entity");
    has_position = false;
    if (args.contains("position") && get_position(args["position"], position)) {
        has_position = true;
    } else if (args.contains("from_position") && get_position(args["from_position"], position)) {
        has_position = true;
    }
    return entity != 0 || has_position;
}

void task_play_anim(INativeApi* api, std::uint32_t ped, const std::string& dict, const std::string& name,
                    int duration_ms, int flags, int ik_flags = 0, float speed = 8.0f,
                    float speed_multiplier = -8.0f) {
    api->invoke(hash::REQUEST_ANIM_DICT, {NativeArg::pointer(const_cast<char*>(dict.c_str()))});
    api->invoke(hash::TASK_PLAY_ANIM, {
        NativeArg::u(ped),
        NativeArg::pointer(const_cast<char*>(dict.c_str())),
        NativeArg::pointer(const_cast<char*>(name.c_str())),
        NativeArg::f(speed),
        NativeArg::f(speed_multiplier),
        NativeArg::i(duration_ms),
        NativeArg::i(flags),
        NativeArg::f(0.0f),
        NativeArg::i(0),          // p8
        NativeArg::i(ik_flags),
        NativeArg::i(0),          // p10
        NativeArg::pointer(const_cast<char*>("")),
        NativeArg::i(0),          // p12
    });
}

struct GestureAnimation {
    std::string dict;
    std::string name;
    int flags;
    int duration_ms;
};

GestureAnimation animation_for_gesture(const std::string& style, int duration_ms) {
    const int flags = ANIM_FLAG_UPPERBODY | ANIM_FLAG_GESTURE | ANIM_FLAG_NOT_INTERRUPTABLE;
    if (style == "nod") return {"ai_gestures@script_story@ridentalk", "positive_nodding_001", flags, duration_ms};
    if (style == "shrug") return {"ai_gestures@script_story@ridentalk", "positive_shrug_001", flags, duration_ms};
    if (style == "friendly") return {"ai_gestures@script_story@ridentalk", "positive_nodding_001", flags, duration_ms};
    if (style == "annoyed") return {"ai_gestures@script_story@ridentalk", "negative_headshake_001", flags, duration_ms};
    if (style == "afraid") return {"g_speak_talk", "g_speak_talk_head_enter", flags, duration_ms};
    if (style == "wave") return {"g_speak_talk", "g_speak_talk_rhand_enter", flags, duration_ms};
    if (style == "think" || style == "ponder")
        return {"ai_gestures@gen_male@standing@silent", style == "ponder" ? "aknwoledge_timid_look_down_f_001"
                                                                          : "aknwoledge_tough_chin_scratch_l_001",
                flags, duration_ms};
    if (style == "scheme")
        return {"script_mp@emotes@scheme@male@unarmed@upper", "loop", ANIM_FLAG_UPPERBODY | ANIM_FLAG_GESTURE, duration_ms};
    if (style == "listen") return {"g_speak_talk", "g_speak_talk_head_enter", flags, duration_ms};
    return {"g_speak_talk", "g_speak_talk_head_enter", flags, duration_ms};
}

bool dispatch_common(INativeApi* api, const ActionRequest& request, const Json& args, ActionResult& out) {
    const std::string& tool = request.tool;
    const std::uint32_t ped = request_ped(request, api);
    if (ped == 0) {
        out.status = "failed";
        out.reason = "invalid_npc_id";
        return false;
    }

    auto fail = [&](const std::string& reason) {
        out.status = "failed";
        out.reason = reason;
        return false;
    };

    if (tool == "go_to") {
        Vec3 destination;
        if (get_destination(api, args, destination)) {
            api->invoke(hash::TASK_GO_TO_COORD_ANY_MEANS, {
                NativeArg::u(ped), NativeArg::f(destination.x), NativeArg::f(destination.y), NativeArg::f(destination.z),
                NativeArg::f(2.0f), NativeArg::u(0), NativeArg::i(0), NativeArg::i(0), NativeArg::f(0.0f),
            });
            out.status = "started";
            return true;
        }
        std::uint32_t entity = resolve_entity_arg(api, args, "destination");
        if (entity == 0 && args.contains("entity")) entity = resolve_entity_arg(api, args, "entity");
        if (entity != 0) {
            api->invoke(hash::TASK_GO_TO_ENTITY, {
                NativeArg::u(ped), NativeArg::u(entity), NativeArg::i(-1), NativeArg::f(2.0f),
                NativeArg::f(0.5f), NativeArg::f(0.0f), NativeArg::i(0),
            });
            out.status = "started";
            return true;
        }
        return fail("destination_unresolved");
    }

    if (tool == "wander") {
        float radius = args.contains("radius") ? args["radius"].as_float(8.0f) : 8.0f;
        Vec3 position;
        if (api->getEntityPosition(ped, position)) {
            api->invoke(hash::TASK_WANDER_IN_AREA, {
                NativeArg::u(ped), NativeArg::f(position.x), NativeArg::f(position.y), NativeArg::f(position.z),
                NativeArg::f(radius), NativeArg::f(3.0f), NativeArg::f(6.0f), NativeArg::i(0),
            });
        } else {
            api->invoke(hash::TASK_WANDER_STANDARD, {NativeArg::u(ped), NativeArg::f(10.0f), NativeArg::i(10)});
        }
        out.status = "started";
        return true;
    }

    if (tool == "follow") {
        std::uint32_t entity = resolve_entity_arg(api, args, "entity");
        if (entity == 0) return fail("entity_unresolved");
        float distance = args.contains("distance") ? args["distance"].as_float(1.5f) : 1.5f;
        api->invoke(hash::TASK_FOLLOW_TO_OFFSET_OF_ENTITY, {
            NativeArg::u(ped), NativeArg::u(entity), NativeArg::f(0.0f), NativeArg::f(distance), NativeArg::f(0.0f),
            NativeArg::f(2.0f), NativeArg::i(-1), NativeArg::f(1.0f), NativeArg::i(1),
            NativeArg::i(0), NativeArg::i(1), NativeArg::i(0), NativeArg::i(0), NativeArg::i(0),
        });
        out.status = "started";
        return true;
    }

    if (tool == "stop") {
        api->invoke(hash::CLEAR_PED_TASKS, {NativeArg::u(ped), NativeArg::i(1), NativeArg::i(1)});
        api->invoke(hash::CLEAR_PED_SECONDARY_TASK, {NativeArg::u(ped)});
        out.status = "started";
        return true;
    }

    if (tool == "wait") {
        float duration = args.contains("duration") ? args["duration"].as_float(2.0f) : 2.0f;
        api->invoke(hash::TASK_PAUSE, {NativeArg::u(ped), NativeArg::i(static_cast<int>(duration * 1000.0f))});
        out.status = "started";
        return true;
    }

    if (tool == "look_at") {
        if (args.contains("entity")) {
            std::uint32_t entity = resolve_entity_arg(api, args, "entity");
            if (entity == 0) return fail("entity_unresolved");
            api->invoke(hash::TASK_LOOK_AT_ENTITY, {
                NativeArg::u(ped), NativeArg::u(entity), NativeArg::i(-1), NativeArg::i(0), NativeArg::i(51), NativeArg::i(0),
            });
        } else if (args.contains("position")) {
            Vec3 position;
            if (!get_position(args["position"], position)) return fail("position_invalid");
            api->invoke(hash::TASK_LOOK_AT_COORD, {
                NativeArg::u(ped), NativeArg::f(position.x), NativeArg::f(position.y), NativeArg::f(position.z),
                NativeArg::i(-1), NativeArg::i(0), NativeArg::i(51), NativeArg::i(0),
            });
        } else {
            return fail("entity_or_position_required");
        }
        out.status = "started";
        return true;
    }

    if (tool == "clear_attention") {
        api->invoke(hash::TASK_CLEAR_LOOK_AT, {NativeArg::u(ped)});
        out.status = "started";
        return true;
    }

    if (tool == "face") {
        if (args.contains("entity")) {
            std::uint32_t entity = resolve_entity_arg(api, args, "entity");
            if (entity == 0) return fail("entity_unresolved");
            api->invoke(hash::TASK_TURN_PED_TO_FACE_ENTITY, {
                NativeArg::u(ped), NativeArg::u(entity), NativeArg::i(3000), NativeArg::f(0.0f),
                NativeArg::f(0.0f), NativeArg::f(0.0f),
            });
        } else if (args.contains("position")) {
            Vec3 position;
            if (!get_position(args["position"], position)) return fail("position_invalid");
            api->invoke(hash::TASK_TURN_PED_TO_FACE_COORD, {
                NativeArg::u(ped), NativeArg::f(position.x), NativeArg::f(position.y), NativeArg::f(position.z),
                NativeArg::i(3000),
            });
        } else {
            return fail("entity_or_position_required");
        }
        out.status = "started";
        return true;
    }

    if (tool == "say") {
        std::string text = args.contains("text") ? args["text"].as_string() : "";
        if (text.empty()) return fail("say_missing_text");
        std::string voice = args.contains("voice") ? args["voice"].as_string() : "";
        struct SpeechParams {
            const char* speech_name;
            const char* voice_name;
            int variation;
            int pad0;
            std::uint32_t speech_param_hash;
            std::uint32_t pad1;
            int listener_ped;
            int pad2;
            int sync_over_network;
            int pad3;
            int v7;
            int pad4;
            int v8;
            int pad5;
        } params{};
        params.speech_name = text.c_str();
        params.voice_name = voice.c_str();
        params.variation = 1;
        params.speech_param_hash = 0x3CA9FB81;  // eSpeechParams.Standard
        params.sync_over_network = 1;
        params.v7 = 1;
        api->invoke(hash::PLAY_PED_AMBIENT_SPEECH_NATIVE, {NativeArg::u(ped), NativeArg::pointer(&params)});
        out.status = "started";
        return true;
    }

    if (tool == "gesture" || tool == "think") {
        std::string style = args.contains("style") ? args["style"].as_string() : (tool == "think" ? "think" : "neutral");
        float duration_s = args.contains("duration") ? args["duration"].as_float(2.5f) : 2.5f;
        GestureAnimation anim = animation_for_gesture(style, static_cast<int>(duration_s * 1000.0f));
        if (duration_s <= 0.0f) anim.duration_ms = -1;
        task_play_anim(api, ped, anim.dict, anim.name, anim.duration_ms, anim.flags);
        out.status = "started";
        return true;
    }

    if (tool == "investigate") {
        Vec3 position;
        if (!get_position(args.contains("position") ? args["position"] : args, position)) {
            return fail("position_invalid");
        }
        api->invoke(hash::TASK_GO_TO_COORD_ANY_MEANS, {
            NativeArg::u(ped), NativeArg::f(position.x), NativeArg::f(position.y), NativeArg::f(position.z),
            NativeArg::f(2.0f), NativeArg::u(0), NativeArg::i(0), NativeArg::i(0), NativeArg::f(0.0f),
        });
        api->invoke(hash::TASK_LOOK_AT_COORD, {
            NativeArg::u(ped), NativeArg::f(position.x), NativeArg::f(position.y), NativeArg::f(position.z),
            NativeArg::i(8000), NativeArg::i(0), NativeArg::i(51), NativeArg::i(0),
        });
        out.status = "started";
        return true;
    }

    if (tool == "flee_from") {
        std::uint32_t entity = resolve_entity_arg(api, args, "entity");
        if (entity != 0) {
            api->invoke(hash::TASK_FLEE_PED, {
                NativeArg::u(ped), NativeArg::u(entity), NativeArg::i(0), NativeArg::i(0), NativeArg::f(-1.0f),
                NativeArg::i(-1), NativeArg::i(0),
            });
        } else if (args.contains("position")) {
            Vec3 position;
            if (!get_position(args["position"], position)) return fail("position_invalid");
            api->invoke(hash::TASK_FLEE_COORD, {
                NativeArg::u(ped), NativeArg::f(position.x), NativeArg::f(position.y), NativeArg::f(position.z),
                NativeArg::i(0), NativeArg::i(0), NativeArg::f(-1.0f), NativeArg::i(-1), NativeArg::i(0),
            });
        } else {
            return fail("entity_or_position_required");
        }
        out.status = "started";
        return true;
    }

    if (tool == "walk_away") {
        std::uint32_t entity = resolve_entity_arg(api, args, "entity");
        if (entity == 0) return fail("entity_unresolved");
        api->invoke(hash::TASK_WALK_AWAY, {NativeArg::u(ped), NativeArg::u(entity)});
        out.status = "started";
        return true;
    }

    if (tool == "react") {
        std::uint32_t entity = resolve_entity_arg(api, args, "entity");
        std::string reaction = args.contains("reaction") ? args["reaction"].as_string() : "DEFAULT";
        Vec3 position{};
        bool has_position = args.contains("position") && get_position(args["position"], position);
        api->invoke(hash::TASK_REACT, {
            NativeArg::u(ped), NativeArg::u(entity),
            NativeArg::f(has_position ? position.x : 0.0f),
            NativeArg::f(has_position ? position.y : 0.0f),
            NativeArg::f(has_position ? position.z : 0.0f),
            NativeArg::pointer(const_cast<char*>(reaction.c_str())),
            NativeArg::f(2.0f), NativeArg::f(0.5f), NativeArg::i(4),
        });
        out.status = "started";
        return true;
    }

    if (tool == "hands_up") {
        float duration = args.contains("duration") ? args["duration"].as_float(3.0f) : 3.0f;
        std::uint32_t facing = resolve_entity_arg(api, args, "face_entity");
        api->invoke(hash::TASK_HANDS_UP, {
            NativeArg::u(ped), NativeArg::i(static_cast<int>(duration * 1000.0f)), NativeArg::u(facing),
            NativeArg::i(-1), NativeArg::i(0),
        });
        out.status = "started";
        return true;
    }

    if (tool == "cower") {
        float duration = args.contains("duration") ? args["duration"].as_float(3.0f) : 3.0f;
        std::uint32_t from = resolve_entity_arg(api, args, "from_entity");
        api->invoke(hash::TASK_COWER, {
            NativeArg::u(ped), NativeArg::i(static_cast<int>(duration * 1000.0f)), NativeArg::u(from),
            NativeArg::pointer(const_cast<char*>("")),
        });
        out.status = "started";
        return true;
    }

    if (tool == "duck") {
        float duration = args.contains("duration") ? args["duration"].as_float(2.0f) : 2.0f;
        api->invoke(hash::TASK_DUCK, {NativeArg::u(ped), NativeArg::i(static_cast<int>(duration * 1000.0f))});
        out.status = "started";
        return true;
    }

    if (tool == "jump") {
        api->invoke(hash::TASK_JUMP, {NativeArg::u(ped), NativeArg::i(1)});
        out.status = "started";
        return true;
    }

    if (tool == "mount") {
        std::uint32_t entity = resolve_entity_arg(api, args, "entity");
        if (entity == 0) return fail("entity_unresolved");
        api->invoke(hash::TASK_MOUNT_ANIMAL, {
            NativeArg::u(ped), NativeArg::u(entity), NativeArg::i(-1), NativeArg::i(-1), NativeArg::f(2.0f),
            NativeArg::i(0), NativeArg::i(0), NativeArg::i(0),
        });
        out.status = "started";
        return true;
    }

    if (tool == "dismount") {
        api->invoke(hash::TASK_DISMOUNT_ANIMAL, {
            NativeArg::u(ped), NativeArg::i(0), NativeArg::i(0), NativeArg::i(0), NativeArg::i(0), NativeArg::u(0),
        });
        out.status = "started";
        return true;
    }

    if (tool == "item_interaction") {
        std::string item = args.contains("item") ? args["item"].as_string() : "";
        std::string interaction = args.contains("interaction") ? args["interaction"].as_string() : "";
        if (item.empty() || interaction.empty()) return fail("item_or_interaction_missing");
        api->invoke(hash::START_TASK_ITEM_INTERACTION, {
            NativeArg::u(ped), NativeArg::u(joaat(item)), NativeArg::u(joaat(interaction)),
            NativeArg::i(1), NativeArg::i(0), NativeArg::f(-1.0f),
        });
        out.status = "started";
        return true;
    }

    if (tool == "animal_interaction") {
        std::string target = args.contains("target") ? args["target"].as_string() : "";
        std::string interaction_type = args.contains("interaction_type") ? args["interaction_type"].as_string() : "";
        std::string interaction_model = args.contains("interaction_model") ? args["interaction_model"].as_string() : "";
        std::uint32_t target_handle = api->resolveEntity(target);
        if (target_handle == 0 || interaction_type.empty() || interaction_model.empty()) {
            return fail("animal_interaction_args_invalid");
        }
        api->invoke(hash::TASK_ANIMAL_INTERACTION, {
            NativeArg::u(ped), NativeArg::u(target_handle), NativeArg::u(joaat(interaction_type)),
            NativeArg::u(joaat(interaction_model)), NativeArg::i(0),
        });
        out.status = "started";
        return true;
    }

    if (tool == "horse_action") {
        int action = args.contains("action") ? args["action"].as_int() : 0;
        std::uint32_t target = resolve_entity_arg(api, args, "target");
        api->invoke(hash::TASK_HORSE_ACTION, {NativeArg::u(ped), NativeArg::i(action), NativeArg::u(target), NativeArg::i(0)});
        out.status = "started";
        return true;
    }

    if (tool == "aim_at") {
        std::uint32_t entity = resolve_entity_arg(api, args, "entity");
        float duration = args.contains("duration") ? args["duration"].as_float(3.0f) : 3.0f;
        if (entity != 0) {
            api->invoke(hash::TASK_AIM_GUN_AT_ENTITY, {
                NativeArg::u(ped), NativeArg::u(entity), NativeArg::i(static_cast<int>(duration * 1000.0f)),
                NativeArg::i(0), NativeArg::i(1),
            });
        } else if (args.contains("position")) {
            Vec3 position;
            if (!get_position(args["position"], position)) return fail("position_invalid");
            api->invoke(hash::TASK_AIM_GUN_AT_COORD_HASH, {
                NativeArg::u(ped), NativeArg::f(position.x), NativeArg::f(position.y), NativeArg::f(position.z),
                NativeArg::i(static_cast<int>(duration * 1000.0f)), NativeArg::i(0), NativeArg::i(0),
            });
        } else {
            return fail("entity_or_position_required");
        }
        out.status = "started";
        return true;
    }

    if (tool == "shoot_at") {
        std::uint32_t entity = resolve_entity_arg(api, args, "entity");
        float duration = args.contains("duration") ? args["duration"].as_float(0.5f) : 0.5f;
        if (entity != 0) {
            api->invoke(hash::TASK_SHOOT_AT_ENTITY, {
                NativeArg::u(ped), NativeArg::u(entity), NativeArg::i(static_cast<int>(duration * 1000.0f)),
                NativeArg::u(0), NativeArg::i(1),
            });
        } else if (args.contains("position")) {
            Vec3 position;
            if (!get_position(args["position"], position)) return fail("position_invalid");
            api->invoke(hash::TASK_SHOOT_AT_COORD_HASH, {
                NativeArg::u(ped), NativeArg::f(position.x), NativeArg::f(position.y), NativeArg::f(position.z),
                NativeArg::i(static_cast<int>(duration * 1000.0f)), NativeArg::u(0), NativeArg::i(0),
            });
        } else {
            return fail("entity_or_position_required");
        }
        out.status = "started";
        return true;
    }

    if (tool == "attack") {
        std::uint32_t entity = resolve_entity_arg(api, args, "entity");
        if (entity == 0) return fail("entity_unresolved");
        api->invoke(hash::TASK_COMBAT_PED_TIMED, {
            NativeArg::u(ped), NativeArg::u(entity), NativeArg::i(5000), NativeArg::i(0),
        });
        out.status = "started";
        return true;
    }

    if (tool == "take_cover") {
        std::uint32_t entity = resolve_entity_arg(api, args, "from_entity");
        if (entity != 0) {
            api->invoke(hash::TASK_SEEK_COVER_FROM_PED, {
                NativeArg::u(ped), NativeArg::u(entity), NativeArg::i(5000), NativeArg::i(0), NativeArg::i(0), NativeArg::i(1),
            });
        } else if (args.contains("from_position")) {
            Vec3 position;
            if (!get_position(args["from_position"], position)) return fail("position_invalid");
            api->invoke(hash::TASK_SEEK_COVER_FROM_POS, {
                NativeArg::u(ped), NativeArg::f(position.x), NativeArg::f(position.y), NativeArg::f(position.z),
                NativeArg::i(5000), NativeArg::i(0), NativeArg::i(0), NativeArg::i(0),
            });
        } else {
            return fail("cover_source_required");
        }
        out.status = "started";
        return true;
    }

    return fail("unsupported_tool");
}

}  // namespace

ActionExecutor::ActionExecutor(INativeApi* native_api) : api_(native_api) {}

bool ActionExecutor::execute(const ActionRequest& request, ActionResult& out_result) {
    out_result.npc_id = request.npc_id;
    out_result.tool = request.tool;
    out_result.request_id = request.request_id;

    if (api_ == nullptr) {
        out_result.status = "failed";
        out_result.reason = "native_api_not_initialized";
        return false;
    }

    Json args = Json::object();
    if (!request.arguments_json.empty()) {
        try {
            args = Json::parse(request.arguments_json);
        } catch (const std::exception&) {
            out_result.status = "failed";
            out_result.reason = "invalid_arguments_json";
            return false;
        }
        if (!args.is_object()) {
            out_result.status = "failed";
            out_result.reason = "arguments_must_be_object";
            return false;
        }
    }
    return dispatch_common(api_, request, args, out_result);
}

void ActionExecutor::on_tick() {
    // Reserved for multi-tick action completion tracking.
}

}  // namespace rdr2ai
