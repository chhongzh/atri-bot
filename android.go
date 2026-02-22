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
*/
import "C"

import (
	"os"
	"unsafe"
)

//export Java_dev_chhongzh_atri_bot_Bridge_Start
func Java_dev_chhongzh_atri_bot_Bridge_Start(env *C.JNIEnv, clazz C.jclass, workingDir C.jstring) /* isSuccess */ C.jboolean {
	cDir := C.GoStringFromJString(env, workingDir)
	dir := "."
	if cDir != nil {
		defer C.free(unsafe.Pointer(cDir))
		dir = C.GoString(cDir)
	}
	if dir != "" {
		_ = os.Chdir(dir)
	}

	isSuccess := commonMain()
	if isSuccess {
		return C.JNI_TRUE
	}
	return C.JNI_FALSE
}
