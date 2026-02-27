//go:build android

package main

/*
#include <jni.h>
#include <stdlib.h>
#include <string.h>

static char* GoStringFromJString(JNIEnv* env, jstring str) {
	if (str == NULL) {
		return NULL;
	}
	const char* utf = (*env)->GetStringUTFChars(env, str, 0);
	if (utf == NULL) {
		return NULL;
	}
	size_t len = strlen(utf);
	char* ret = (char*)malloc(len + 1);
	if (ret == NULL) {
		(*env)->ReleaseStringUTFChars(env, str, utf);
		return NULL;
	}
	memcpy(ret, utf, len + 1);
	(*env)->ReleaseStringUTFChars(env, str, utf);
	return ret;
}

static jstring JStringFromCString(JNIEnv* env, const char* str) {
	if (str == NULL) {
		return NULL;
	}
	return (*env)->NewStringUTF(env, str);
}
*/
import "C"

import (
	"context"
	"fmt"
	"os"
	"unsafe"
)

var androidCtx context.Context
var androidCancel context.CancelFunc

//export Java_dev_chhongzh_atri_1bot_1launcher_Bridge_Start
func Java_dev_chhongzh_atri_1bot_1launcher_Bridge_Start(env *C.JNIEnv, clazz C.jclass, workingDir C.jstring) /* isSuccess */ C.jboolean {
	cDir := C.GoStringFromJString(env, workingDir)
	if cDir == nil {
		// 报错1
		errBuf <- fmt.Errorf("Java层传递到C层的dir是空的")
		return C.JNI_FALSE
	}
	defer C.free(unsafe.Pointer(cDir))
	dir = C.GoString(cDir)
	if dir == "" {
		// 报错2
		errBuf <- fmt.Errorf("dir 是空的")
		return C.JNI_FALSE

	}
	err := os.Chdir(dir)
	if err != nil {
		// 报错2
		errBuf <- fmt.Errorf("chdir错误: %w", err)
		return C.JNI_FALSE
	}

	if androidCancel != nil {
		androidCancel()
	}
	androidCtx, androidCancel = context.WithCancel(context.Background())

	isSuccess := commonMain(androidCtx)
	if isSuccess {
		return C.JNI_TRUE
	}
	return C.JNI_FALSE
}

//export Java_dev_chhongzh_atri_1bot_1launcher_Bridge_Stop
func Java_dev_chhongzh_atri_1bot_1launcher_Bridge_Stop(env *C.JNIEnv, clazz C.jclass) {
	if androidCancel != nil {
		androidCancel()
		androidCancel = nil
		androidCtx = nil
	}
}

//export Java_dev_chhongzh_atri_1bot_1launcher_Bridge_PollResult
func Java_dev_chhongzh_atri_1bot_1launcher_Bridge_PollResult(env *C.JNIEnv, clazz C.jclass) C.jstring {
	s := <-errBuf
	cs := C.CString(s.Error())
	defer C.free(unsafe.Pointer(cs))
	return C.JStringFromCString(env, cs)
}
