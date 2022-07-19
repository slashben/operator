package mainhandler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"k8s-ca-websocket/cautils"
	"net/http"
	"net/url"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/strings/slices"

	pkgwlid "github.com/armosec/utils-k8s-go/wlid"
	uuid "github.com/google/uuid"

	"github.com/armosec/armoapi-go/apis"
	"github.com/armosec/armoapi-go/armotypes"
	"github.com/armosec/k8s-interface/cloudsupport"
	"github.com/armosec/k8s-interface/k8sinterface"
	"github.com/golang/glog"
)

const dockerPullableURN = "docker-pullable://"

var (
	vulnScanHttpClient = &http.Client{}
)

func getVulnScanURL() *url.URL {
	vulnURL := url.URL{}
	vulnURL.Scheme = "http"
	vulnURL.Host = cautils.ClusterConfig.VulnScanURL
	vulnURL.Path = fmt.Sprintf("%s/%s", apis.WebsocketScanCommandVersion, apis.WebsocketScanCommandPath)
	return &vulnURL
}
func sendAllImagesToVulnScan(webSocketScanCMDList []*apis.WebsocketScanCommand) error {
	var err error
	errs := make([]error, 0)
	for _, webSocketScanCMD := range webSocketScanCMDList {
		err = sendWorkloadToVulnerabilityScanner(webSocketScanCMD)
		if err != nil {
			glog.Errorf("sendWorkloadToVulnerabilityScanner failed with err %v", err)
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		err = fmt.Errorf("sendAllImagesToVulnScan errors: ")
		for errIdx := range errs {
			err = fmt.Errorf("%s; %w", err, errs[errIdx])
		}
		return err
	}
	return nil
}

// in what scenario this function returns error???
func convertImagesToWebsocketScanCommand(registry *registryScan, sessionObj *cautils.SessionObj) ([]*apis.WebsocketScanCommand, error) {
	images := registry.mapImageToTags

	webSocketScanCMDList := make([]*apis.WebsocketScanCommand, 0)
	for repository, tags := range images {
		// registry/project/repo --> repo
		repositoryName := strings.Replace(repository, registry.registry.hostname+"/"+registry.registry.projectID+"/", "", -1)
		for _, tag := range tags {
			glog.Info("image ", repository+":"+tag)
			websocketScanCommand := &apis.WebsocketScanCommand{
				ParentJobID: sessionObj.Reporter.GetJobID(),
				JobID:       uuid.NewString(),
				ImageTag:    repository + ":" + tag,
				Session:     apis.SessionChain{ActionTitle: "vulnerability-scan", JobIDs: make([]string, 0), Timestamp: sessionObj.Reporter.GetTimestamp()},
				Args: map[string]interface{}{
					armotypes.AttributeRegistryName:  registry.registry.hostname + "/" + registry.registry.projectID,
					armotypes.AttributeRepository:    repositoryName,
					armotypes.AttributeTag:           tag,
					armotypes.AttributeUseHTTP:       *registry.registryAuth.Insecure,
					armotypes.AttributeSkipTLSVerify: *registry.registryAuth.SkipTLSVerify,
				},
			}
			// Check if auth is empty (used for public registries)
			authConfig := registry.authConfig()
			if authConfig != nil {
				websocketScanCommand.Credentialslist = append(websocketScanCommand.Credentialslist, *authConfig)
			}
			webSocketScanCMDList = append(webSocketScanCMDList, websocketScanCommand)
		}
	}

	return webSocketScanCMDList, nil
}

func (actionHandler *ActionHandler) getRegistryAuth(registryName string) (*registryAuth, error) {
	secret, err := actionHandler.getRegistryScanSecret()
	if err != nil {
		return nil, err
	}
	secretData := secret.GetData()
	var registriesAuth []registryAuth
	registriesAuthStr, ok := secretData[registriesAuthFieldInSecret].(string)
	if !ok {
		return nil, fmt.Errorf("error parsing Secret: %s field must be a string", registriesAuthFieldInSecret)
	}
	data, err := base64.StdEncoding.DecodeString(registriesAuthStr)
	if err != nil {
		return nil, fmt.Errorf("error parsing Secret: %s", err.Error())
	}
	registriesAuthStr = strings.Replace(string(data), "\n", "", -1)
	err = json.Unmarshal([]byte(registriesAuthStr), &registriesAuth)
	if err != nil {
		return nil, fmt.Errorf("error parsing Secret: %s", err.Error())
	}
	//try to find an auth with the same registry name from the request
	for _, auth := range registriesAuth {
		if auth.Registry == registryName {
			if err := auth.initDefaultValues(); err != nil {
				return nil, err
			}
			return &auth, nil
		}
	}
	//couldn't find auth with the full, check if there is an auth for the registry without the project name
	regAndProject := strings.Split(registryName, "/")
	if len(regAndProject) > 1 {
		for _, auth := range registriesAuth {
			if auth.Registry == regAndProject[0] {
				if err := auth.initDefaultValues(); err != nil {
					return nil, err
				}
				return &auth, nil
			}
		}

	}
	//no auth found for registry return a default one
	auth := makeRegistryAuth(registryName)
	return &auth, nil
}

func (actionHandler *ActionHandler) getRegistryConfig(registryName string) (*registryScanConfig, bool, error) {
	configMap, err := actionHandler.k8sAPI.GetWorkload(armoNamespace, "ConfigMap", registryScanConfigmap)
	if err != nil {
		// if configmap not found, it means we will use all images and default depth
		if strings.Contains(err.Error(), fmt.Sprintf("reason: configmaps \"%v\" not found", registryScanConfigmap)) {
			glog.Infof("configmap: %s does not exists, using default values", registryScanConfigmap)
			return NewRegistryScanConfig(registryName), false, nil
		} else {
			return nil, false, err
		}
	}
	configData := configMap.GetData()
	var registriesConfigs []registryScanConfig
	registriesConfigStr, ok := configData["registries"].(string)
	if !ok {
		return nil, false, fmt.Errorf("error parsing %v confgimap: registries field not found", registryScanConfigmap)
	}
	registriesConfigStr = strings.Replace(registriesConfigStr, "\n", "", -1)
	err = json.Unmarshal([]byte(registriesConfigStr), &registriesConfigs)
	if err != nil {
		return nil, false, fmt.Errorf("error parsing ConfigMap: %s", err.Error())
	}
	for _, config := range registriesConfigs {
		if config.Registry == registryName {
			return &config, true, nil
		}
	}
	return NewRegistryScanConfig(registryName), false, nil

}

func (actionHandler *ActionHandler) getRegistryScanSecret() (k8sinterface.IWorkload, error) {
	secret, err := actionHandler.k8sAPI.GetWorkload(armoNamespace, "Secret", registryScanSecret)
	return secret, err

}

func (actionHandler *ActionHandler) scanRegistries(sessionObj *cautils.SessionObj) error {

	/*
		Auth data must be stored in kubescape-registry-scan secret
		Config data must be stored in "kubescape-registry-scan" config map
	*/

	registryName, err := actionHandler.parseRegistryNameArg(sessionObj)
	if err != nil {
		glog.Errorf("parseRegistryNameArg failed with err %v", err)
		return err
	}
	auth, err := actionHandler.getRegistryAuth(registryName)
	if err != nil {
		glog.Errorf("get registry auth failed with err %v", err)
		return err
	}
	sessionObj.Reporter.SendDetails("secret loaded", true, sessionObj.ErrChan)
	glog.Infof("scanRegistries: %s secret parsing successful", registryScanSecret)

	conf, isLoaded, err := actionHandler.getRegistryConfig(registryName)
	if err != nil {
		glog.Errorf("get registry(%s) config failed with err %v", registryName, err) //systest depedendent
		return err
	}
	cmLoadMode := "default"
	if isLoaded {
		cmLoadMode = "loaded"
	}
	glog.Infof("scanRegistries:registry(%s) %s configmap  successful", registryName, cmLoadMode) // systest
	registryScan := NewRegistryScan(registryName, *auth, *conf)
	return actionHandler.scanRegistry(&registryScan, sessionObj)
}

func (actionHandler *ActionHandler) parseRegistryNameArg(sessionObj *cautils.SessionObj) (string, error) {
	registryInfo, ok := sessionObj.Command.Args[registryInfoV1].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("could not parse registry info")
	}
	registryName, ok := registryInfo[registryNameField].(string)
	if !ok {
		return "", fmt.Errorf("could not parse registry name")
	}

	sessionObj.Reporter.SetTarget(fmt.Sprintf("%s: %s", armotypes.AttributeRegistryName,
		registryName))
	sessionObj.Reporter.SendDetails(fmt.Sprintf("registryInfo parsed: %v", registryInfo), true, sessionObj.ErrChan)
	return registryName, nil
}

func (actionHandler *ActionHandler) scanRegistry(registry *registryScan, sessionObj *cautils.SessionObj) error {
	err := registry.getImagesForScanning(actionHandler.reporter)
	if err != nil {
		glog.Errorf("GetImagesForScanning failed with err %v", err)
		return err
	}
	webSocketScanCMDList, err := convertImagesToWebsocketScanCommand(registry, sessionObj)
	if err != nil {
		glog.Errorf("convertImagesToWebsocketScanCommand failed with err %v", err)
		return err
	}
	sessionObj.Reporter.SendDetails(fmt.Sprintf("sending %d images from registry %v to vuln scan", len(webSocketScanCMDList), registry.registry), true, sessionObj.ErrChan)
	err = sendAllImagesToVulnScan(webSocketScanCMDList)
	if err != nil {
		glog.Errorf("sendAllImagesToVulnScanByMemLimit failed with err %v", err)
	}
	return err
}

func (actionHandler *ActionHandler) scanWorkload(sessionObj *cautils.SessionObj) error {

	pod, err := actionHandler.getPodByWLID(actionHandler.wlid)
	if err != nil {
		glog.Errorf("scanning might fail if some images require credentials")
	}
	// get all images of workload
	errs := ""
	containers, err := getWorkloadImages(actionHandler.k8sAPI, actionHandler.wlid)
	if err != nil {
		return fmt.Errorf("failed to get workloads from k8s, wlid: %s, reason: %s", actionHandler.wlid, err.Error())
	}

	// we want running pod in order to have the image hash
	actionHandler.getRunningPodDescription(pod)

	for i := range containers {

		websocketScanCommand := &apis.WebsocketScanCommand{
			Wlid:          actionHandler.wlid,
			ImageTag:      containers[i].image,
			ContainerName: containers[i].container,
			Session:       apis.SessionChain{ActionTitle: "vulnerability-scan", JobIDs: make([]string, 0), Timestamp: sessionObj.Reporter.GetTimestamp()},
		}
		if actionHandler.reporter != nil {

			prepareSessionChain(sessionObj, websocketScanCommand, actionHandler)

			glog.Infof("wlid: %s, container: %s, image: %s, jobIDs: %s/%s/%s", websocketScanCommand.Wlid, websocketScanCommand.ContainerName, websocketScanCommand.ImageTag, actionHandler.reporter.GetParentAction(), websocketScanCommand.ParentJobID, websocketScanCommand.JobID)

			// if websocketScanCommand.ParentJobID != actionHandler.command.JobTracking.ParentID {
			// 	glog.Errorf("websocket command parent: %v, child: %v, VS actionhandler.command parent: %v child %v", websocketScanCommand.ParentJobID, websocketScanCommand.JobID, actionHandler.command.JobTracking.ParentID, actionHandler.command.JobTracking.JobID)
			// }
		}
		for contIdx := range pod.Status.ContainerStatuses {
			if pod.Status.ContainerStatuses[contIdx].Name == containers[i].container {
				imageNameWithHash := pod.Status.ContainerStatuses[contIdx].ImageID
				imageNameWithHash = strings.TrimPrefix(imageNameWithHash, dockerPullableURN)
				websocketScanCommand.ImageHash = imageNameWithHash
			}
		}
		if pod != nil {
			secrets, err := cloudsupport.GetImageRegistryCredentials(websocketScanCommand.ImageTag, pod)
			if err != nil {
				glog.Error(err)
			} else if len(secrets) > 0 {
				for secretName := range secrets {
					websocketScanCommand.Credentialslist = append(websocketScanCommand.Credentialslist, secrets[secretName])
				}

				/*
					the websocketScanCommand.Credentials is depracated, still use it for backward compstability
				*/
				if len(websocketScanCommand.Credentialslist) != 0 {
					websocketScanCommand.Credentials = &websocketScanCommand.Credentialslist[0]
				}
			}
		}
		if err := sendWorkloadToVulnerabilityScanner(websocketScanCommand); err != nil {
			glog.Errorf("scanning %v failed due to: %v", websocketScanCommand.ImageTag, err.Error())
			errs += fmt.Sprintf("failed scanning, wlid: '%s', image: '%s', container: %s, reason: %s", actionHandler.wlid, containers[i].image, containers[i].container, err.Error())

		}

	}
	if errs != "" {
		return fmt.Errorf(errs)
	}
	return nil
}

func prepareSessionChain(sessionObj *cautils.SessionObj, websocketScanCommand *apis.WebsocketScanCommand, actionHandler *ActionHandler) {
	sessParentJobId := sessionObj.Reporter.GetParentAction()
	if sessParentJobId != "" {
		websocketScanCommand.Session.JobIDs = append(websocketScanCommand.Session.JobIDs, sessParentJobId)
		websocketScanCommand.Session.RootJobID = sessParentJobId
	}
	sessJobID := sessionObj.Reporter.GetJobID()
	if websocketScanCommand.Session.RootJobID == "" {
		websocketScanCommand.Session.RootJobID = sessJobID
	}
	websocketScanCommand.Session.JobIDs = append(websocketScanCommand.Session.JobIDs, sessJobID)

	if actionHandler.reporter.GetParentAction() != "" && !slices.Contains(websocketScanCommand.Session.JobIDs, actionHandler.reporter.GetParentAction()) {
		websocketScanCommand.Session.JobIDs = append(websocketScanCommand.Session.JobIDs, actionHandler.reporter.GetParentAction())
	}

	if actionHandler.reporter.GetJobID() != "" && !slices.Contains(websocketScanCommand.Session.JobIDs, actionHandler.reporter.GetJobID()) {
		websocketScanCommand.Session.JobIDs = append(websocketScanCommand.Session.JobIDs, actionHandler.reporter.GetJobID())
	}

	websocketScanCommand.ParentJobID = actionHandler.reporter.GetJobID()
	websocketScanCommand.LastAction = actionHandler.reporter.GetActionIDN()
	websocketScanCommand.JobID = uuid.NewString()
	websocketScanCommand.Session.JobIDs = append(websocketScanCommand.Session.JobIDs, websocketScanCommand.JobID)
}

func (actionHandler *ActionHandler) getRunningPodDescription(pod *corev1.Pod) {
	if workloadObj, err := actionHandler.k8sAPI.GetWorkloadByWlid(actionHandler.wlid); err == nil {
		if selectors, err := workloadObj.GetSelector(); err == nil {
			gvr, _ := k8sinterface.GetGroupVersionResource("Pod")
			podList, err := actionHandler.k8sAPI.ListWorkloads(&gvr, workloadObj.GetNamespace(), selectors.MatchLabels, map[string]string{"status.phase": "Running"})
			if err == nil {
				if len(podList) > 0 {
					wlidKind := pkgwlid.GetKindFromWlid(actionHandler.wlid)
					wlidName := pkgwlid.GetNameFromWlid(actionHandler.wlid)
					for podIdx := range podList {
						parentKind, parentName, err := actionHandler.k8sAPI.CalculateWorkloadParentRecursive(podList[podIdx])
						if err == nil && parentKind == wlidKind && wlidName == parentName {
							podBts, err := json.Marshal(podList[podIdx].GetObject())
							if err != nil {
								continue
							}
							err = json.Unmarshal(podBts, pod)
							if err != nil {
								continue
							}
							break
						}
					}
				}
			}
		}
	}
}

func sendWorkloadToVulnerabilityScanner(websocketScanCommand *apis.WebsocketScanCommand) error {

	jsonScannerC, err := json.Marshal(websocketScanCommand)
	if err != nil {
		return err
	}
	vulnURL := getVulnScanURL()

	creds := websocketScanCommand.Credentials
	credsList := websocketScanCommand.Credentialslist
	hasCreds := creds != nil && len(creds.Username) > 0 && len(creds.Password) > 0 || len(credsList) > 0
	glog.Infof("requesting scan. url: %s wlid: %s image: %s with credentials: %v", vulnURL.String(), websocketScanCommand.Wlid, websocketScanCommand.ImageTag, hasCreds)

	req, err := http.NewRequest("POST", vulnURL.String(), bytes.NewBuffer(jsonScannerC))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := vulnScanHttpClient.Do(req)
	refusedNum := 0
	for ; refusedNum < 5 && err != nil && strings.Contains(err.Error(), "connection refused"); resp, err = vulnScanHttpClient.Do(req) {
		glog.Errorf("failed posting to vulnerability scanner. query: '%s', reason: %s", websocketScanCommand.ImageTag, err.Error())
		time.Sleep(5 * time.Second)
		refusedNum++
	}
	if err != nil {
		return fmt.Errorf("failed posting to vulnerability scanner. query: '%s', reason: %s", websocketScanCommand.ImageTag, err.Error())
	}
	if resp == nil {
		return fmt.Errorf("failed posting to vulnerability scanner. query: '%s', reason: 'empty response'", websocketScanCommand.ImageTag)
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}

	if resp.StatusCode < 200 || resp.StatusCode > 203 {
		return fmt.Errorf("failed posting to vulnerability scanner. query: '%s', reason: 'received bad status code: %d'", websocketScanCommand.ImageTag, resp.StatusCode)
	}
	return nil
}

func (actionHandler *ActionHandler) getPodByWLID(wlid string) (*corev1.Pod, error) {
	var err error
	workload, err := actionHandler.k8sAPI.GetWorkloadByWlid(actionHandler.wlid)
	if err != nil {
		return nil, err
	}
	podspec, err := workload.GetPodSpec()
	if err != nil {
		return nil, err
	}
	podObj := &corev1.Pod{Spec: *podspec}
	podObj.ObjectMeta.Namespace = pkgwlid.GetNamespaceFromWlid(wlid)
	return podObj, nil
}
