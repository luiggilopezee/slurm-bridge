// SPDX-FileCopyrightText: Copyright (C) SchedMD LLC.
// SPDX-License-Identifier: Apache-2.0

package slurmjobir

import (
	"context"
	"testing"

	"github.com/SlinkyProject/slurm-bridge/internal/wellknown"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newJob(name string) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: metav1.NamespaceDefault,
			Name:      name,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    *resource.NewQuantity(22, resource.DecimalSI),
							corev1.ResourceMemory: *resource.NewQuantity(1024*1024, resource.DecimalSI),
						},
					},
				},
			},
		},
	}
}

func newJobPod(name, jobName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: metav1.NamespaceDefault,
			Name:      name,
			Labels: map[string]string{
				"job-name": jobName,
			},
		},
	}
}

func TestTranslateJobMailAnnotations(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		wantUser    string
		wantType    []string
		wantErr     bool
	}{
		{name: "root fallback", wantUser: "root@nih.gov", wantType: []string{"END"}},
		{name: "template overrides", annotations: map[string]string{
			wellknown.AnnotationMailUser: "template@nih.gov",
			wellknown.AnnotationMailType: "BEGIN,FAIL",
			wellknown.AnnotationAccount:  "pod-account",
		}, wantUser: "template@nih.gov", wantType: []string{"BEGIN", "FAIL"}},
		{name: "per-key fallback", annotations: map[string]string{wellknown.AnnotationMailUser: "template@nih.gov"}, wantUser: "template@nih.gov", wantType: []string{"END"}},
		{name: "explicit none", annotations: map[string]string{wellknown.AnnotationMailType: "NONE"}, wantUser: "root@nih.gov", wantType: []string{"NONE"}},
		{name: "invalid template", annotations: map[string]string{wellknown.AnnotationMailUser: "bad"}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			job := newJob("mail-job")
			job.Annotations = map[string]string{
				wellknown.AnnotationMailUser: "root@nih.gov",
				wellknown.AnnotationMailType: "END",
				wellknown.AnnotationAccount:  "root-account",
			}
			job.Spec.Template.Annotations = test.annotations
			pod := newJobPod("mail-pod", job.Name)
			pod.Annotations = job.Spec.Template.Annotations
			pod.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(job, batchv1.SchemeGroupVersion.WithKind("Job"))}
			reader := fake.NewClientBuilder().WithObjects(job, pod).Build()
			got, err := TranslateToSlurmJobIR(reader, context.Background(), pod)
			if (err != nil) != test.wantErr {
				t.Fatalf("TranslateToSlurmJobIR() = %v, wantErr %v", err, test.wantErr)
			}
			if err != nil {
				return
			}
			if ptr.Deref(got.JobInfo.MailUser, "") != test.wantUser || !apiequality.Semantic.DeepEqual(got.JobInfo.MailType, test.wantType) {
				t.Errorf("unexpected mail settings: %+v", got.JobInfo)
			}
			if ptr.Deref(got.JobInfo.Account, "") != "root-account" {
				t.Error("mail annotations changed existing account precedence")
			}
		})
	}
}

func Test_translator_fromJob(t *testing.T) {
	type fields struct {
		Reader client.Reader
		ctx    context.Context
	}
	type args struct {
		pod     *corev1.Pod
		rootPOM *metav1.PartialObjectMetadata
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    *SlurmJobIR
		wantErr bool
	}{
		{
			name: "Job does not exist",
			fields: fields{
				Reader: func() client.Reader {
					scheme := runtime.NewScheme()
					utilruntime.Must(kubescheme.AddToScheme(scheme))
					utilruntime.Must(batchv1.AddToScheme(scheme))
					return fake.NewClientBuilder().WithScheme(scheme).WithObjects(
						newJob("wrongJob"),
					).Build()
				}(),
				ctx: context.Background(),
			},
			args: args{
				rootPOM: &metav1.PartialObjectMetadata{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "foo",
						Namespace: metav1.NamespaceDefault,
					},
				},
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "Job to SlurmJobIR",
			fields: fields{
				Reader: func() client.Reader {
					scheme := runtime.NewScheme()
					utilruntime.Must(kubescheme.AddToScheme(scheme))
					utilruntime.Must(batchv1.AddToScheme(scheme))
					return fake.NewClientBuilder().WithScheme(scheme).WithObjects(
						newJob("foo"),
						newJobPod("foo", "foo"),
					).Build()
				}(),
				ctx: context.Background(),
			},
			args: args{
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "foo",
						Namespace: metav1.NamespaceDefault,
						Labels:    map[string]string{batchv1.JobNameLabel: "foo"},
					},
				},
				rootPOM: &metav1.PartialObjectMetadata{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "foo",
						Namespace: metav1.NamespaceDefault,
					},
				},
			},
			want: &SlurmJobIR{
				JobInfo: SlurmJobIRJobInfo{
					MinNodes:   ptr.To(int32(1)),
					CpuPerTask: ptr.To(int32(22)),
					MemPerNode: ptr.To(int64(1)),
				},
				Pods: corev1.PodList{
					Items: []corev1.Pod{{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: metav1.NamespaceDefault,
							Labels:    map[string]string{batchv1.JobNameLabel: "foo"},
						},
					}},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &translator{
				Reader: tt.fields.Reader,
				ctx:    tt.fields.ctx,
			}
			got, err := tr.fromJob(tt.args.pod, tt.args.rootPOM)
			if (err != nil) != tt.wantErr {
				t.Errorf("translator.fromJob() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !apiequality.Semantic.DeepEqual(got, tt.want) {
				t.Errorf("translator.fromJob() = %v, want %v", got, &tt.want)
			}
		})
	}
}
