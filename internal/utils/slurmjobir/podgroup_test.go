// SPDX-FileCopyrightText: Copyright (C) SchedMD LLC.
// SPDX-License-Identifier: Apache-2.0

package slurmjobir

import (
	"context"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	schedulingv1alpha2 "k8s.io/api/scheduling/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	fwk "k8s.io/kube-scheduler/framework"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"

	"github.com/SlinkyProject/slurm-bridge/internal/wellknown"
)

func podWithSchedulingGroup(ns, name, pgName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec: corev1.PodSpec{
			SchedulingGroup: &corev1.PodSchedulingGroup{
				PodGroupName: ptr.To(pgName),
			},
		},
	}
}

func newPodGroup(name, ns string, policy schedulingv1alpha2.PodGroupSchedulingPolicy) *schedulingv1alpha2.PodGroup {
	return &schedulingv1alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: schedulingv1alpha2.PodGroupSpec{
			SchedulingPolicy: policy,
		},
	}
}

func Test_translator_fromPodGroup(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(schedulingv1alpha2.AddToScheme(scheme))

	type args struct {
		pod     *corev1.Pod
		rootPOM *metav1.PartialObjectMetadata
	}
	tests := []struct {
		name    string
		client  client.Client
		args    args
		wantErr bool
	}{
		{
			name: "two pods same scheduling group",
			client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(
				newPodGroup("pg1", "default", schedulingv1alpha2.PodGroupSchedulingPolicy{
					Gang: &schedulingv1alpha2.GangSchedulingPolicy{MinCount: 2},
				}),
				podWithSchedulingGroup("default", "p1", "pg1"),
				podWithSchedulingGroup("default", "p2", "pg1"),
			).Build(),
			args: args{
				pod: podWithSchedulingGroup("default", "p1", "pg1"),
				rootPOM: &metav1.PartialObjectMetadata{
					TypeMeta:   podgroup_v1alpha2,
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "pg1"},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := translator{Reader: tt.client, ctx: context.TODO()}
			got, err := tr.fromPodGroup(tt.args.pod, tt.args.rootPOM)
			if (err != nil) != tt.wantErr {
				t.Fatalf("fromPodGroup() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(got.Pods.Items) != 2 {
				t.Errorf("fromPodGroup() len(pods) = %d, want 2", len(got.Pods.Items))
			}
			if got.JobInfo.JobName == nil || *got.JobInfo.JobName != "pg1" {
				t.Errorf("fromPodGroup() JobName = %v, want pg1", got.JobInfo.JobName)
			}
		})
	}
}

func Test_translator_PreFilterPodGroup(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(schedulingv1alpha2.AddToScheme(scheme))

	pg := newPodGroup("pg1", "default", schedulingv1alpha2.PodGroupSchedulingPolicy{
		Gang: &schedulingv1alpha2.GangSchedulingPolicy{MinCount: 2},
	})
	p1 := podWithSchedulingGroup("default", "p1", "pg1")
	p2 := podWithSchedulingGroup("default", "p2", "pg1")

	type args struct {
		pod        *corev1.Pod
		slurmJobIR *SlurmJobIR
	}
	tests := []struct {
		name   string
		client client.Client
		args   args
		want   *fwk.Status
	}{
		{
			name:   "gang satisfied",
			client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(pg.DeepCopy()).Build(),
			args: args{
				pod: p1.DeepCopy(),
				slurmJobIR: &SlurmJobIR{
					RootPOM: metav1.PartialObjectMetadata{
						TypeMeta: podgroup_v1alpha2,
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "default",
							Name:      "pg1",
						},
					},
					Pods: corev1.PodList{Items: []corev1.Pod{*p1, *p2}},
				},
			},
			want: fwk.NewStatus(fwk.Success),
		},
		{
			name:   "gang not enough pods",
			client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(pg.DeepCopy()).Build(),
			args: args{
				pod: p1.DeepCopy(),
				slurmJobIR: &SlurmJobIR{
					RootPOM: metav1.PartialObjectMetadata{
						TypeMeta: podgroup_v1alpha2,
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "default",
							Name:      "pg1",
						},
					},
					Pods: corev1.PodList{Items: []corev1.Pod{*p1}},
				},
			},
			want: fwk.NewStatus(fwk.Error, ErrorInsuffientPods.Error()),
		},
		{
			name: "basic policy skips gang count",
			client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(
				newPodGroup("pg2", "default", schedulingv1alpha2.PodGroupSchedulingPolicy{
					Basic: &schedulingv1alpha2.BasicSchedulingPolicy{},
				}),
			).Build(),
			args: args{
				pod: podWithSchedulingGroup("default", "p1", "pg2"),
				slurmJobIR: &SlurmJobIR{
					RootPOM: metav1.PartialObjectMetadata{
						TypeMeta: podgroup_v1alpha2,
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "default",
							Name:      "pg2",
						},
					},
					Pods: corev1.PodList{Items: []corev1.Pod{*podWithSchedulingGroup("default", "p1", "pg2")}},
				},
			},
			want: fwk.NewStatus(fwk.Success),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := translator{Reader: tt.client, ctx: context.TODO()}
			got := tr.PreFilterPodGroup(tt.args.pod, tt.args.slurmJobIR)
			if !got.Equal(tt.want) {
				t.Errorf("PreFilterPodGroup() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_schedulingGroupsMatch(t *testing.T) {
	a := &corev1.PodSchedulingGroup{PodGroupName: ptr.To("pg1")}
	b := &corev1.PodSchedulingGroup{PodGroupName: ptr.To("pg1")}
	c := &corev1.PodSchedulingGroup{PodGroupName: ptr.To("pg2")}
	if !schedulingGroupsMatch(a, b) {
		t.Error("expected same group")
	}
	if schedulingGroupsMatch(a, c) {
		t.Error("expected different groups not to match")
	}
}

func jobWithAnnotations(name, ns string, ann map[string]string) *batchv1.Job {
	return &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			APIVersion: batchv1.SchemeGroupVersion.String(),
			Kind:       "Job",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   ns,
			Annotations: ann,
		},
	}
}

func podWithJobOwner(pod *corev1.Pod, jobName string) *corev1.Pod {
	out := pod.DeepCopy()
	out.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: batchv1.SchemeGroupVersion.String(),
		Kind:       "Job",
		Name:       jobName,
		Controller: ptr.To(true),
	}}
	return out
}

func TestTranslateToSlurmJobIR_PodGroupMailAnnotations(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(schedulingv1alpha2.AddToScheme(scheme))

	for _, policy := range []schedulingv1alpha2.PodGroupSchedulingPolicy{
		{Basic: &schedulingv1alpha2.BasicSchedulingPolicy{}},
		{Gang: &schedulingv1alpha2.GangSchedulingPolicy{MinCount: 1}},
	} {
		pg := newPodGroup("pg", "default", policy)
		pg.Spec.PodGroupTemplateRef = &schedulingv1alpha2.PodGroupTemplateReference{
			Workload: &schedulingv1alpha2.WorkloadPodGroupTemplateReference{
				WorkloadName: "workload", PodGroupTemplateName: "workers",
			},
		}
		workload := &schedulingv1alpha2.Workload{
			ObjectMeta: metav1.ObjectMeta{Name: "workload", Namespace: "default", Annotations: map[string]string{
				wellknown.AnnotationMailUser: "invalid-inherited-address",
				wellknown.AnnotationMailType: "END",
				wellknown.AnnotationQOS:      "workload-qos",
			}},
		}
		job := jobWithAnnotations("job", "default", nil)
		pod := podWithJobOwner(podWithSchedulingGroup("default", "pod", pg.Name), job.Name)
		pod.Annotations = map[string]string{
			wellknown.AnnotationMailUser: "pod@nih.gov",
			wellknown.AnnotationQOS:      "ignored-pod-qos",
		}
		reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pg, workload, job, pod).Build()
		ir, err := TranslateToSlurmJobIR(reader, context.Background(), pod)
		if err != nil {
			t.Fatal(err)
		}
		if ptr.Deref(ir.JobInfo.MailUser, "") != "pod@nih.gov" || len(ir.JobInfo.MailType) != 1 || ir.JobInfo.MailType[0] != "END" {
			t.Errorf("mail annotations did not preserve Pod override and Workload fallback: %#v", ir.JobInfo)
		}
		if ptr.Deref(ir.JobInfo.QOS, "") != "workload-qos" {
			t.Errorf("non-mail annotation precedence changed: %#v", ir.JobInfo)
		}
	}
}

func TestTranslateToSlurmJobIR_PodGroupEffectiveMail(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(schedulingv1alpha2.AddToScheme(scheme))
	tests := []struct {
		name            string
		key             string
		inheritedSource string
		inheritedValue  string
		overrideSource  string
		overrideValue   string
		omitWorkload    bool
		omitWorkloadRef bool
		wantErr         bool
	}{
		{name: "controller overrides invalid PodGroup event", key: wellknown.AnnotationMailType, inheritedSource: "podgroup", inheritedValue: "SUBMIT", overrideSource: "controller", overrideValue: "END"},
		{name: "workload overrides invalid controller recipient", key: wellknown.AnnotationMailUser, inheritedSource: "controller", inheritedValue: "invalid", overrideSource: "workload", overrideValue: "workload@example.org"},
		{name: "pod END overrides invalid workload event", key: wellknown.AnnotationMailType, inheritedSource: "workload", inheritedValue: "SUBMIT", overrideSource: "pod", overrideValue: "END"},
		{name: "pod NONE overrides invalid workload event", key: wellknown.AnnotationMailType, inheritedSource: "workload", inheritedValue: "SUBMIT", overrideSource: "pod", overrideValue: "NONE"},
		{name: "pod recipient overrides invalid workload recipient", key: wellknown.AnnotationMailUser, inheritedSource: "workload", inheritedValue: "invalid", overrideSource: "pod", overrideValue: "pod@example.org"},
		{name: "invalid inherited event without override fails", key: wellknown.AnnotationMailType, inheritedSource: "podgroup", inheritedValue: "SUBMIT", wantErr: true},
		{name: "invalid inherited recipient without override fails", key: wellknown.AnnotationMailUser, inheritedSource: "workload", inheritedValue: "invalid", wantErr: true},
		{name: "invalid Pod event overrides valid inherited event", key: wellknown.AnnotationMailType, inheritedSource: "workload", inheritedValue: "END", overrideSource: "pod", overrideValue: "SUBMIT", wantErr: true},
		{name: "invalid Pod recipient overrides valid inherited recipient", key: wellknown.AnnotationMailUser, inheritedSource: "controller", inheritedValue: "controller@example.org", overrideSource: "pod", overrideValue: "invalid", wantErr: true},
		{name: "no Workload reference still applies Pod override", key: wellknown.AnnotationMailType, inheritedSource: "podgroup", inheritedValue: "SUBMIT", overrideSource: "pod", overrideValue: "NONE", omitWorkloadRef: true},
		{name: "missing Workload still applies Pod override", key: wellknown.AnnotationMailType, inheritedSource: "controller", inheritedValue: "SUBMIT", overrideSource: "pod", overrideValue: "END", omitWorkload: true},
		{name: "invalid inherited ordinary policy still fails", key: wellknown.AnnotationTimeLimit, inheritedSource: "podgroup", inheritedValue: "invalid", overrideSource: "workload", overrideValue: "10", wantErr: true},
	}
	for _, policy := range []struct {
		name  string
		value schedulingv1alpha2.PodGroupSchedulingPolicy
	}{
		{name: "basic", value: schedulingv1alpha2.PodGroupSchedulingPolicy{Basic: &schedulingv1alpha2.BasicSchedulingPolicy{}}},
		{name: "gang", value: schedulingv1alpha2.PodGroupSchedulingPolicy{Gang: &schedulingv1alpha2.GangSchedulingPolicy{MinCount: 1}}},
	} {
		for _, test := range tests {
			t.Run(policy.name+"/"+test.name, func(t *testing.T) {
				podGroup := newPodGroup("pg", "default", policy.value)
				podGroup.Annotations = map[string]string{
					wellknown.AnnotationMailUser: "group@example.org",
					wellknown.AnnotationMailType: "FAIL",
				}
				if !test.omitWorkloadRef {
					podGroup.Spec.PodGroupTemplateRef = &schedulingv1alpha2.PodGroupTemplateReference{
						Workload: &schedulingv1alpha2.WorkloadPodGroupTemplateReference{
							WorkloadName: "workload", PodGroupTemplateName: "workers",
						},
					}
				}
				workload := &schedulingv1alpha2.Workload{
					ObjectMeta: metav1.ObjectMeta{Name: "workload", Namespace: "default", Annotations: map[string]string{}},
				}
				job := jobWithAnnotations("job", "default", map[string]string{wellknown.AnnotationAccount: "controller-account"})
				pod := podWithJobOwner(podWithSchedulingGroup("default", "pod", podGroup.Name), job.Name)
				pod.Annotations = map[string]string{wellknown.AnnotationAccount: "ignored-pod-account"}
				sources := map[string]map[string]string{
					"podgroup": podGroup.Annotations, "controller": job.Annotations,
					"workload": workload.Annotations, "pod": pod.Annotations,
				}
				sources[test.inheritedSource][test.key] = test.inheritedValue
				if test.overrideSource != "" {
					sources[test.overrideSource][test.key] = test.overrideValue
				}
				objects := []client.Object{podGroup, job, pod}
				if !test.omitWorkload {
					objects = append(objects, workload)
				}
				reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
				jobIR, err := TranslateToSlurmJobIR(reader, context.Background(), pod)
				if (err != nil) != test.wantErr {
					t.Fatalf("TranslateToSlurmJobIR() error = %v, wantErr %v", err, test.wantErr)
				}
				if err != nil {
					return
				}
				wantUser, wantType := "group@example.org", "FAIL"
				if test.key == wellknown.AnnotationMailUser {
					wantUser = test.overrideValue
				} else {
					wantType = test.overrideValue
				}
				if ptr.Deref(jobIR.JobInfo.MailUser, "") != wantUser || len(jobIR.JobInfo.MailType) != 1 || jobIR.JobInfo.MailType[0] != wantType {
					t.Errorf("unexpected effective mail settings: %+v", jobIR.JobInfo)
				}
				if ptr.Deref(jobIR.JobInfo.Account, "") != "controller-account" {
					t.Errorf("ordinary policy precedence changed: %+v", jobIR.JobInfo)
				}
			})
		}
	}
}

func TestTranslateToSlurmJobIR_PodGroupAnnotations(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(schedulingv1alpha2.AddToScheme(scheme))
	utilruntime.Must(batchv1.AddToScheme(scheme))
	utilruntime.Must(jobset.AddToScheme(scheme))

	workloadRef := &schedulingv1alpha2.PodGroupTemplateReference{
		Workload: &schedulingv1alpha2.WorkloadPodGroupTemplateReference{
			WorkloadName:         "my-workload",
			PodGroupTemplateName: "workers",
		},
	}

	tests := []struct {
		name          string
		objects       []client.Object
		wantJobName   string
		wantTimeLimit *int32
		wantPartition *string
		wantQOS       *string
		wantAccount   *string
		wantNoAccount bool
		wantNoWckey   bool
	}{
		{
			name: "jobset is selected instead of intermediate job",
			objects: []client.Object{
				func() *schedulingv1alpha2.PodGroup {
					pg := newPodGroup("pg1", "default", schedulingv1alpha2.PodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha2.GangSchedulingPolicy{MinCount: 2},
					})
					pg.Annotations = map[string]string{
						wellknown.AnnotationPartition: "podgroup-partition",
						wellknown.AnnotationQOS:       "podgroup-qos",
					}
					pg.Spec.PodGroupTemplateRef = workloadRef
					return pg
				}(),
				&schedulingv1alpha2.Workload{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-workload",
						Namespace: "default",
						Annotations: map[string]string{
							wellknown.AnnotationQOS: "workload-qos",
						},
					},
				},
				&jobset.JobSet{
					TypeMeta: jobSet_v1alpha2,
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-jobset",
						Namespace: "default",
						Annotations: map[string]string{
							wellknown.AnnotationPartition: "jobset-partition",
							wellknown.AnnotationQOS:       "jobset-qos",
						},
					},
				},
				func() *batchv1.Job {
					job := jobWithAnnotations("my-job", "default", map[string]string{
						wellknown.AnnotationPartition: "job-partition",
						wellknown.AnnotationQOS:       "job-qos",
						wellknown.AnnotationAccount:   "intermediate-job-account",
					})
					job.OwnerReferences = []metav1.OwnerReference{
						controllerOwner(jobSet_v1alpha2.APIVersion, jobSet_v1alpha2.Kind, "my-jobset"),
					}
					return job
				}(),
				func() *corev1.Pod {
					pod := podWithJobOwner(podWithSchedulingGroup("default", "p1", "pg1"), "my-job")
					pod.Annotations = map[string]string{wellknown.AnnotationWckey: "pod-wckey"}
					return pod
				}(),
				podWithJobOwner(podWithSchedulingGroup("default", "p2", "pg1"), "my-job"),
			},
			wantJobName:   "pg1",
			wantPartition: ptr.To("jobset-partition"),
			wantQOS:       ptr.To("workload-qos"),
			wantNoAccount: true,
			wantNoWckey:   true,
		},
		{
			name: "workload overrides job and podgroup",
			objects: []client.Object{
				func() *schedulingv1alpha2.PodGroup {
					pg := newPodGroup("pg1", "default", schedulingv1alpha2.PodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha2.GangSchedulingPolicy{MinCount: 2},
					})
					pg.Annotations = map[string]string{wellknown.AnnotationTimeLimit: "10"}
					pg.Spec.PodGroupTemplateRef = workloadRef
					return pg
				}(),
				&schedulingv1alpha2.Workload{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-workload", Namespace: "default",
						Annotations: map[string]string{wellknown.AnnotationTimeLimit: "30"},
					},
				},
				jobWithAnnotations("my-job", "default", map[string]string{
					wellknown.AnnotationJobName:   "my-job",
					wellknown.AnnotationTimeLimit: "5",
				}),
				podWithJobOwner(podWithSchedulingGroup("default", "p1", "pg1"), "my-job"),
				podWithJobOwner(podWithSchedulingGroup("default", "p2", "pg1"), "my-job"),
			},
			wantJobName:   "my-job",
			wantTimeLimit: ptr.To(int32(30)),
		},
		{
			name: "job overrides podgroup without workload ref",
			objects: []client.Object{
				func() *schedulingv1alpha2.PodGroup {
					pg := newPodGroup("pg1", "default", schedulingv1alpha2.PodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha2.GangSchedulingPolicy{MinCount: 2},
					})
					pg.Annotations = map[string]string{wellknown.AnnotationTimeLimit: "10"}
					return pg
				}(),
				jobWithAnnotations("my-job", "default", map[string]string{
					wellknown.AnnotationTimeLimit: "5",
				}),
				podWithJobOwner(podWithSchedulingGroup("default", "p1", "pg1"), "my-job"),
				podWithJobOwner(podWithSchedulingGroup("default", "p2", "pg1"), "my-job"),
			},
			wantJobName:   "pg1",
			wantTimeLimit: ptr.To(int32(5)),
		},
		{
			name: "podgroup only when job has no annotations",
			objects: []client.Object{
				func() *schedulingv1alpha2.PodGroup {
					pg := newPodGroup("pg1", "default", schedulingv1alpha2.PodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha2.GangSchedulingPolicy{MinCount: 2},
					})
					pg.Annotations = map[string]string{
						wellknown.AnnotationTimeLimit: "10",
						wellknown.AnnotationPartition: "pg-partition",
					}
					return pg
				}(),
				jobWithAnnotations("my-job", "default", nil),
				podWithJobOwner(podWithSchedulingGroup("default", "p1", "pg1"), "my-job"),
				podWithJobOwner(podWithSchedulingGroup("default", "p2", "pg1"), "my-job"),
			},
			wantJobName:   "pg1",
			wantTimeLimit: ptr.To(int32(10)),
			wantPartition: ptr.To("pg-partition"),
		},
		{
			name: "missing workload falls back to job over podgroup",
			objects: []client.Object{
				func() *schedulingv1alpha2.PodGroup {
					pg := newPodGroup("pg1", "default", schedulingv1alpha2.PodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha2.GangSchedulingPolicy{MinCount: 2},
					})
					pg.Annotations = map[string]string{wellknown.AnnotationTimeLimit: "10"}
					pg.Spec.PodGroupTemplateRef = workloadRef
					return pg
				}(),
				jobWithAnnotations("my-job", "default", map[string]string{
					wellknown.AnnotationTimeLimit: "5",
				}),
				podWithJobOwner(podWithSchedulingGroup("default", "p1", "pg1"), "my-job"),
				podWithJobOwner(podWithSchedulingGroup("default", "p2", "pg1"), "my-job"),
			},
			wantJobName:   "pg1",
			wantTimeLimit: ptr.To(int32(5)),
		},
		{
			name: "non-conflicting annotations from each layer",
			objects: []client.Object{
				func() *schedulingv1alpha2.PodGroup {
					pg := newPodGroup("pg1", "default", schedulingv1alpha2.PodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha2.GangSchedulingPolicy{MinCount: 2},
					})
					pg.Annotations = map[string]string{wellknown.AnnotationPartition: "pg-partition"}
					pg.Spec.PodGroupTemplateRef = workloadRef
					return pg
				}(),
				&schedulingv1alpha2.Workload{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-workload", Namespace: "default",
						Annotations: map[string]string{wellknown.AnnotationAccount: "wl-account"},
					},
				},
				jobWithAnnotations("my-job", "default", map[string]string{
					wellknown.AnnotationQOS: "job-qos",
				}),
				podWithJobOwner(podWithSchedulingGroup("default", "p1", "pg1"), "my-job"),
				podWithJobOwner(podWithSchedulingGroup("default", "p2", "pg1"), "my-job"),
			},
			wantJobName:   "pg1",
			wantPartition: ptr.To("pg-partition"),
			wantQOS:       ptr.To("job-qos"),
			wantAccount:   ptr.To("wl-account"),
		},
		{
			name: "workload wins on same key as job and podgroup",
			objects: []client.Object{
				func() *schedulingv1alpha2.PodGroup {
					pg := newPodGroup("pg1", "default", schedulingv1alpha2.PodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha2.GangSchedulingPolicy{MinCount: 2},
					})
					pg.Annotations = map[string]string{wellknown.AnnotationPartition: "pg-partition"}
					pg.Spec.PodGroupTemplateRef = workloadRef
					return pg
				}(),
				&schedulingv1alpha2.Workload{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-workload", Namespace: "default",
						Annotations: map[string]string{wellknown.AnnotationPartition: "wl-partition"},
					},
				},
				jobWithAnnotations("my-job", "default", map[string]string{
					wellknown.AnnotationPartition: "job-partition",
				}),
				podWithJobOwner(podWithSchedulingGroup("default", "p1", "pg1"), "my-job"),
				podWithJobOwner(podWithSchedulingGroup("default", "p2", "pg1"), "my-job"),
			},
			wantJobName:   "pg1",
			wantPartition: ptr.To("wl-partition"),
		},
		{
			name: "workload job-name overrides podgroup object name when they differ",
			objects: []client.Object{
				func() *schedulingv1alpha2.PodGroup {
					pg := newPodGroup("training-job-workers", "default", schedulingv1alpha2.PodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha2.GangSchedulingPolicy{MinCount: 2},
					})
					pg.Spec.PodGroupTemplateRef = &schedulingv1alpha2.PodGroupTemplateReference{
						Workload: &schedulingv1alpha2.WorkloadPodGroupTemplateReference{
							WorkloadName:         "training-workload",
							PodGroupTemplateName: "workers",
						},
					}
					return pg
				}(),
				&schedulingv1alpha2.Workload{
					ObjectMeta: metav1.ObjectMeta{
						Name: "training-workload", Namespace: "default",
						Annotations: map[string]string{
							wellknown.AnnotationJobName: "training-job",
						},
					},
				},
				jobWithAnnotations("training-job", "default", nil),
				podWithJobOwner(podWithSchedulingGroup("default", "p1", "training-job-workers"), "training-job"),
				podWithJobOwner(podWithSchedulingGroup("default", "p2", "training-job-workers"), "training-job"),
			},
			wantJobName: "training-job",
		},
		{
			name: "workload job-name overrides podgroup job-name annotation",
			objects: []client.Object{
				func() *schedulingv1alpha2.PodGroup {
					pg := newPodGroup("training-job-workers", "default", schedulingv1alpha2.PodGroupSchedulingPolicy{
						Gang: &schedulingv1alpha2.GangSchedulingPolicy{MinCount: 2},
					})
					pg.Annotations = map[string]string{
						wellknown.AnnotationJobName: "pg-slurm-name",
					}
					pg.Spec.PodGroupTemplateRef = &schedulingv1alpha2.PodGroupTemplateReference{
						Workload: &schedulingv1alpha2.WorkloadPodGroupTemplateReference{
							WorkloadName:         "training-workload",
							PodGroupTemplateName: "workers",
						},
					}
					return pg
				}(),
				&schedulingv1alpha2.Workload{
					ObjectMeta: metav1.ObjectMeta{
						Name: "training-workload", Namespace: "default",
						Annotations: map[string]string{
							wellknown.AnnotationJobName: "training-job",
						},
					},
				},
				jobWithAnnotations("training-job", "default", nil),
				podWithJobOwner(podWithSchedulingGroup("default", "p1", "training-job-workers"), "training-job"),
				podWithJobOwner(podWithSchedulingGroup("default", "p2", "training-job-workers"), "training-job"),
			},
			wantJobName: "training-job",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objs := make([]client.Object, len(tt.objects))
			for i, obj := range tt.objects {
				objs[i] = obj.DeepCopyObject().(client.Object)
			}
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()

			pod := tt.objects[len(tt.objects)-2].(*corev1.Pod).DeepCopy()
			got, err := TranslateToSlurmJobIR(cl, context.TODO(), pod)
			if err != nil {
				t.Fatalf("TranslateToSlurmJobIR() error = %v", err)
			}
			if tt.wantJobName != "" {
				if got.JobInfo.JobName == nil || *got.JobInfo.JobName != tt.wantJobName {
					t.Errorf("JobName = %q, want %q", ptr.Deref(got.JobInfo.JobName, ""), tt.wantJobName)
				}
			}
			if tt.wantTimeLimit != nil {
				if got.JobInfo.TimeLimit == nil || *got.JobInfo.TimeLimit != *tt.wantTimeLimit {
					t.Errorf("TimeLimit = %v, want %v", got.JobInfo.TimeLimit, *tt.wantTimeLimit)
				}
			}
			if tt.wantPartition != nil {
				if got.JobInfo.Partition == nil || *got.JobInfo.Partition != *tt.wantPartition {
					t.Errorf("Partition = %v, want %v", got.JobInfo.Partition, *tt.wantPartition)
				}
			}
			if tt.wantQOS != nil {
				if got.JobInfo.QOS == nil || *got.JobInfo.QOS != *tt.wantQOS {
					t.Errorf("QOS = %v, want %v", got.JobInfo.QOS, *tt.wantQOS)
				}
			}
			if tt.wantAccount != nil {
				if got.JobInfo.Account == nil || *got.JobInfo.Account != *tt.wantAccount {
					t.Errorf("Account = %v, want %v", got.JobInfo.Account, *tt.wantAccount)
				}
			}
			if tt.wantNoAccount && got.JobInfo.Account != nil {
				t.Errorf("Account = %q, want intermediate Job annotation ignored", *got.JobInfo.Account)
			}
			if tt.wantNoWckey && got.JobInfo.Wckey != nil {
				t.Errorf("Wckey = %q, want Pod annotation ignored", *got.JobInfo.Wckey)
			}
		})
	}
}
