cmake_minimum_required(VERSION 3.20)
project(rdr2_ai_bridge LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

option(RDR2AI_HAS_SCRIPTHOOK "Build the ASI plugin against the ScriptHookRDR2 SDK" OFF)

set(BRIDGE_CORE_SOURCES
    src/action_executor.cpp
    src/ped_scanner.cpp
    src/story_gate.cpp
    src/ipc_client.cpp
    src/bridge_runtime.cpp
)

add_library(rdr2_ai_bridge_core STATIC ${BRIDGE_CORE_SOURCES})
target_include_directories(rdr2_ai_bridge_core PUBLIC include)
find_package(Threads REQUIRED)
target_link_libraries(rdr2_ai_bridge_core PUBLIC Threads::Threads)

if(RDR2AI_HAS_SCRIPTHOOK)
    if(NOT SCRIPTHOOK_RDR2_ROOT)
        if(DEFINED ENV{RDR2AI_SCRIPTHOOK_ROOT})
            set(SCRIPTHOOK_RDR2_ROOT "$ENV{RDR2AI_SCRIPTHOOK_ROOT}")
        elseif(EXISTS "${CMAKE_CURRENT_SOURCE_DIR}/.sdk/ScriptHookRDR2_SDK_1.0.1207.73")
            set(SCRIPTHOOK_RDR2_ROOT "${CMAKE_CURRENT_SOURCE_DIR}/.sdk/ScriptHookRDR2_SDK_1.0.1207.73")
        endif()
    endif()
    if(NOT SCRIPTHOOK_RDR2_ROOT)
        message(FATAL_ERROR "Set -DSCRIPTHOOK_RDR2_ROOT=... or run scripts/install_scripthook_sdk.py")
    endif()
    message(STATUS "ScriptHookRDR2 SDK root: ${SCRIPTHOOK_RDR2_ROOT}")
    add_library(rdr2_ai_bridge SHARED
        ${BRIDGE_CORE_SOURCES}
        src/scripthook_native_api.cpp
        src/bridge_main.cpp
    )
    target_include_directories(rdr2_ai_bridge PRIVATE
        include
        ${SCRIPTHOOK_RDR2_ROOT}/inc
    )
    target_compile_definitions(rdr2_ai_bridge PRIVATE RDR2AI_HAS_SCRIPTHOOK)
    # ScriptHookRDR2 only loads plugins with the .asi extension, so name the
    # artifact what the game actually looks for.
    set_target_properties(rdr2_ai_bridge PROPERTIES PREFIX "")
    if(WIN32)
        set_target_properties(rdr2_ai_bridge PROPERTIES SUFFIX ".asi")
        target_link_libraries(rdr2_ai_bridge PRIVATE
            ws2_32
            ${SCRIPTHOOK_RDR2_ROOT}/lib/ScriptHookRDR2.lib
        )
    endif()
endif()

enable_testing()
add_executable(bridge_core_test
    tests/bridge_core_test.cpp
    src/action_executor.cpp
    src/ped_scanner.cpp
    src/story_gate.cpp
)
target_include_directories(bridge_core_test PRIVATE include tests)
add_test(NAME bridge_core_test COMMAND bridge_core_test)
