/*
Licensed under the Mozilla Public License (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    https://www.mozilla.org/en-US/MPL/2.0/
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"encoding/json"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/TykTechnologies/tyk-operator/api/v1alpha1"
	"github.com/TykTechnologies/tyk-operator/pkg/cert"
	"github.com/TykTechnologies/tyk-operator/pkg/client/klient"
	"github.com/TykTechnologies/tyk-operator/pkg/environmet"
	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	util "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	certFinalizerName = "finalizers.tyk.io/certs"
	TLSSecretType     = "kubernetes.io/tls"
)

// SecretCertReconciler reconciles a Cert object
type SecretCertReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
	Env    environmet.Env
}

// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;update

func (r *SecretCertReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("cert", req.NamespacedName)
	desired := &v1.Secret{}

	log.Info("getting secret resource")

	if err := r.Get(ctx, req.NamespacedName, desired); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err) // Ignore not-found errors
	}
	// set context for all api calls inside this reconciliation loop
	env, ctx, err := HttpContext(ctx, r.Client, r.Env, desired, log)
	if err != nil {
		return ctrl.Result{}, err
	}

	log.Info("checking secret type is kubernetes.io/tls")

	if desired.Type != TLSSecretType {
		// it's not for us
		return ctrl.Result{}, nil
	}

	// If object is being deleted
	if !desired.ObjectMeta.DeletionTimestamp.IsZero() {
		err = r.delete(ctx, desired, log, env.Org)
		if err != nil {
			return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 5}, err
		}

		return ctrl.Result{}, nil
	}

	util.AddFinalizer(desired, certFinalizerName)

	log.Info("ensuring tls.key is present")

	tlsKey, ok := desired.Data["tls.key"]
	if !ok {
		// cert doesn't exist yet
		log.Info("missing tls.key, we don't care about it yet")
		return ctrl.Result{}, nil
	}

	log.Info("ensuring tls.crt is present")

	tlsCrt, ok := desired.Data["tls.crt"]
	if !ok {
		// cert doesn't exist yet
		log.Info("missing tls.crt, we don't care about it yet")
		return ctrl.Result{}, nil
	}

	// list all apidefinitions in namespace of the secret
	apiDefList := v1alpha1.ApiDefinitionList{}
	opts := []client.ListOption{
		client.InNamespace(req.Namespace),
	}

	if err := r.List(ctx, &apiDefList, opts...); err != nil {
		log.Info("unable to list api definitions")
		return ctrl.Result{}, err
	}

	if len(apiDefList.Items) == 0 {
		log.Info("no apidefinitions in namespace")
		return ctrl.Result{}, nil
	}

	certID := ""
	isCertAlreadyUploaded := false

	for idx := range apiDefList.Items {
		for domain := range apiDefList.Items[idx].Spec.UpstreamCertificateRefs {
			if req.Name == apiDefList.Items[idx].Spec.UpstreamCertificateRefs[domain] {
				// do not upload cert again if it is already uploaded
				if !isCertAlreadyUploaded {
					certID, err = klient.Universal.Certificate().Upload(ctx, tlsKey, tlsCrt)
					if err != nil {
						return ctrl.Result{Requeue: true}, err
					}

					log.Info("uploaded certificate to Tyk", "certID", certID)

					isCertAlreadyUploaded = true
				}

				if apiDefList.Items[idx].Spec.UpstreamCertificates == nil {
					apiDefList.Items[idx].Spec.UpstreamCertificates = make(map[string]string)
				}

				apiDefList.Items[idx].Spec.UpstreamCertificates[domain] = certID

				err = r.Update(ctx, &apiDefList.Items[idx])

				// The Pod has been updated/deleted since we read it.
				// Requeue the Pod to try to reconciliate again.
				if apierrors.IsConflict(err) || apierrors.IsNotFound(err) {
					return ctrl.Result{Requeue: true}, nil
				}

				if err != nil {
					log.Error(err, "Unable to update API Definition with cert id", "apiID", *apiDefList.Items[idx].Spec.APIID)
					return ctrl.Result{Requeue: true}, nil
				}

				log.Info("api def updated successfully")
			}
		}

		for domain := range apiDefList.Items[idx].Spec.PinnedPublicKeysRefs {
			if desired.Name == apiDefList.Items[idx].Spec.PinnedPublicKeysRefs[domain] {
				// do not upload cert again if it is already uploaded
				if !isCertAlreadyUploaded {
					certID, err = klient.Universal.Certificate().Upload(ctx, tlsKey, tlsCrt)
					if err != nil {
						return ctrl.Result{Requeue: true}, err
					}

					log.Info("uploaded certificate to Tyk", "certID", certID)

					isCertAlreadyUploaded = true
				}

				if apiDefList.Items[idx].Spec.PinnedPublicKeys == nil {
					apiDefList.Items[idx].Spec.PinnedPublicKeys = map[string]string{}
				}

				apiDefList.Items[idx].Spec.PinnedPublicKeys[domain] = certID

				err = r.Update(ctx, &apiDefList.Items[idx])

				// The ApiDefinition has been updated/deleted since we read it.
				// Requeue to try to reconciliate again.
				if apierrors.IsConflict(err) || apierrors.IsNotFound(err) {
					return ctrl.Result{Requeue: true}, nil
				}

				if err != nil {
					log.Error(err, "unable to update ApiDef")
					return ctrl.Result{Requeue: true}, nil
				}

				log.Info("ApiDefinition updated successfully")
			}
		}

		if containsString(apiDefList.Items[idx].Spec.CertificateSecretNames, req.Name) {
			if !isCertAlreadyUploaded {
				certID, err = klient.Universal.Certificate().Upload(ctx, tlsKey, tlsCrt)
				if err != nil {
					return ctrl.Result{Requeue: true}, err
				}

				log.Info("uploaded certificate to Tyk", "certID", certID)

				isCertAlreadyUploaded = true
			}

			if apiDefList.Items[idx].Spec.Certificates == nil {
				apiDefList.Items[idx].Spec.Certificates = []string{}
			}

			apiDefList.Items[idx].Spec.Certificates = []string{certID}

			err = r.Update(ctx, &apiDefList.Items[idx])

			// The ApiDefinition has been updated/deleted since we read it.
			// Requeue to try to reconciliate again.
			if apierrors.IsConflict(err) || apierrors.IsNotFound(err) {
				return ctrl.Result{Requeue: true}, nil
			}

			if err != nil {
				log.Error(err, "unable to update ApiDef")
				return ctrl.Result{Requeue: true}, nil
			}

			log.Info("ApiDefinition updated successfully")
		}
	}

	return ctrl.Result{}, nil
}

func (r *SecretCertReconciler) delete(ctx context.Context, desired *v1.Secret, log logr.Logger, orgID string) error {
	log.Info("secret being deleted")
	// If our finalizer is present, need to delete from Tyk still
	if util.ContainsFinalizer(desired, certFinalizerName) {
		log.Info("running finalizer logic")

		certPemBytes, ok := desired.Data["tls.crt"]
		if !ok {
			return nil
		}

		certFingerPrint, err := cert.CalculateFingerPrint(certPemBytes)
		if err != nil {
			log.Error(err, "Failed to delete Tyk certificate")
			return nil
		}

		certID := orgID + certFingerPrint

		log.Info("deleting certificate from tyk certificate manager", "orgID", orgID, "fingerprint", certFingerPrint)

		if err := klient.Universal.Certificate().Delete(ctx, certID); err != nil {
			log.Error(err, "unable to delete certificate")
			return err
		}

		if err := klient.Universal.HotReload(ctx); err != nil {
			return err
		}

		log.Info("removing finalizer from secret")
		util.RemoveFinalizer(desired, certFinalizerName)

		if err := r.Update(ctx, desired); err != nil {
			return err
		}
	}

	log.Info("secret successfully deleted")

	return nil
}

// https://sdk.operatorframework.io/docs/building-operators/golang/tutorial/#resources-watched-by-the-controller
func (r *SecretCertReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Secret{}).
		WithEventFilter(r.ignoreNonTLSPredicate()).
		Complete(r)
}

type mySecretType struct {
	Meta struct {
		Type string `json:"type"`
	} `json:"Meta"`
	MetaNew struct {
		Type string `json:"type"`
	} `json:"MetaNew"`
}

// NewSecretType represents a structure for new Kubernetes Secret Object.
type NewSecretType struct {
	ObjectOld struct {
		Type string `json:"type"`
	} `json:"ObjectOld"`
	ObjectNew struct {
		Type string `json:"type"`
	} `json:"ObjectNew"`
	Object struct {
		Type string `json:"type"`
	} `json:"Object"`
}

func (r *SecretCertReconciler) ignoreNonTLSPredicate() predicate.Predicate {
	// isTLSType filters created secret resources based on its type. Right now, only allowed secret type is
	// kubernetes.io/tls.
	isTLSType := func(jsBytes []byte) bool {
		secret := mySecretType{}

		err := json.Unmarshal(jsBytes, &secret)
		if err != nil {
			return false
		}

		if secret.MetaNew.Type == "" && secret.Meta.Type == "" {
			newSecret := NewSecretType{}

			err := json.Unmarshal(jsBytes, &newSecret)
			if err != nil {
				return false
			}

			return newSecret.ObjectNew.Type == TLSSecretType || newSecret.Object.Type == TLSSecretType
		}

		// if Update
		if secret.MetaNew.Type != "" {
			return secret.MetaNew.Type == TLSSecretType
		}
		// then it's a create / delete op
		return secret.Meta.Type == TLSSecretType
	}
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			eBytes, _ := json.Marshal(e)
			return isTLSType(eBytes)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			eBytes, _ := json.Marshal(e)
			return isTLSType(eBytes)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return true
		},
	}
}
